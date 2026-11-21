package albums

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/albumblob"
	"github.com/libraz/nodate-time/apps/api/internal/blobref"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventacl"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/http/precondition"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

// allowedAlbumImageTypes is an exact allowlist of image formats a browser will
// render inline without also being able to execute active content (unlike
// image/svg+xml, which can carry <script>).
var allowedAlbumImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

const (
	maxPhotoSize = 20 * 1024 * 1024
	// maxThumbnailSize bounds the second, smaller rendering. The photo's own
	// ceiling is the wrong number for it by three orders of magnitude, and a
	// ceiling that admits a full-size picture admits sending the picture twice.
	//
	// The size is derived from what a thumbnail is: 400px on its longest edge,
	// which is the 134px grid tile at the 3x screens this app targets. Even the
	// worst encoding of that -- lossless PNG with an alpha channel, 400x400x4
	// bytes before compression -- fits inside a megabyte, so this refuses a
	// full-size photo in a thumbnail's clothing without ever refusing a real
	// one. Refusing a real one would be the expensive mistake: the thumbnail is
	// declared in the same request as the photo, so the picture would fail to
	// upload over a rendering nothing needed.
	maxThumbnailSize = 1024 * 1024
	uploadTTL        = 15 * time.Minute
	// imageTTL covers the URLs embedded in a listing, which the browser only
	// fetches as thumbnails scroll into view. A page of them is signed in one
	// response but consumed over however long the reader keeps the album open,
	// so a short life here shows up as images that break partway down the
	// grid. An hour outlasts a browsing session; the client re-lists when one
	// does expire.
	imageTTL = time.Hour
	// downloadTTL covers the single URL handed back from an explicit download,
	// which is followed immediately and never revisited.
	downloadTTL     = 5 * time.Minute
	defaultPageSize = 30
	maxPageSize     = 100
)

type Deps struct {
	DB          *sql.DB
	Queries     *generated.Queries
	Storage     *storage.Client
	WorkspaceID uint32
}

func pubIDToHex(b []byte) string {
	return calresolve.PublicIDString(b)
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Read(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// resolveCalendarWrite resolves the calendar and rejects read-only (viewer)
// members, who may read but not mutate calendar content.
func resolveCalendarWrite(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	return calresolve.Write(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

func resolveCalendarMember(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, generated.CalendarMember, error) {
	return calresolve.Member(ctx, deps.Queries, deps.WorkspaceID, calPubID, userID)
}

// isImageContentType checks against an exact allowlist rather than a
// "image/*, except svg" prefix rule: mime.ParseMediaType strips parameters
// first, so a value like "image/svg+xml; charset=utf-8" cannot slip past a
// naive HasPrefix/inequality check the way it could before.
func isImageContentType(ct string) bool {
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return allowedAlbumImageTypes[strings.ToLower(mediaType)]
}

func photoStorageKey(calPubHex, photoPubHex string) string {
	return fmt.Sprintf("albums/%s/%s", calPubHex, photoPubHex)
}

// photoObjectKey says where a photo's bytes are.
//
// A photo that has been moved onto the object model is read through the
// object, because that is the row the sweep and the reference count agree
// about -- and after deduplication it can name another photo's copy of the
// same bytes. A photo the backfill has not reached yet is read through the key
// it was uploaded with, which is what lets the migration stop half-way without
// anything going dark.
//
// The listings resolve the same thing in SQL: a page carries thirty photos and
// this would be thirty lookups.
func photoObjectKey(ctx context.Context, deps Deps, p generated.AlbumPhoto) string {
	if !p.StorageObjectID.Valid {
		return p.StorageKey
	}
	object, err := deps.Queries.GetStorageObjectByID(ctx, uint32(p.StorageObjectID.Int32))
	if err != nil {
		slog.WarnContext(ctx, "album photo storage object is unreadable, falling back to its own key",
			"photoID", p.ID, "storageObjectID", p.StorageObjectID.Int32, "error", err)
		return p.StorageKey
	}
	return object.StorageKey
}

// thumbnailObjectKey says where a photo's grid-sized rendering is, or "" when
// it has none.
//
// There is no fallback key to try, unlike the picture: a thumbnail has only
// ever existed as an object, so a photo without one is simply drawn from the
// picture -- larger than it needs to be, and correct.
//
// The listings resolve this in SQL for the reason photoObjectKey does.
func thumbnailObjectKey(ctx context.Context, deps Deps, p generated.AlbumPhoto) string {
	if !p.ThumbnailObjectID.Valid {
		return ""
	}
	object, err := deps.Queries.GetStorageObjectByID(ctx, uint32(p.ThumbnailObjectID.Int32))
	if err != nil {
		slog.WarnContext(ctx, "album photo thumbnail object is unreadable, falling back to the photo",
			"photoID", p.ID, "thumbnailObjectID", p.ThumbnailObjectID.Int32, "error", err)
		return ""
	}
	return object.StorageKey
}

// photoExtensions maps the allowed album image content types to a file
// extension for the synthesized download filename (album photos have no
// stored original filename, unlike event attachments).
var photoExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// photoDownloadFilename builds a human-readable download filename from a
// photo's caption (if any) and its content type, so a saved file is not just
// an opaque UUID.
func photoDownloadFilename(p generated.AlbumPhoto) string {
	name := strings.TrimSpace(p.Caption)
	if name == "" {
		name = "photo"
	}
	return name + photoExtensions[strings.ToLower(p.ContentType)]
}

// encodeCursor names the last photo of a page by its public id.
//
// The page is ordered by (takenAt, id) and both are needed to resume, but the
// id is an internal sequence and a base64 wrapper is not an opaque cursor --
// it is the same number, spelled differently. Naming the row instead lets the
// server look the pair up, and tells the holder nothing but which photo they
// have already seen.
func encodeCursor(publicID []byte) string {
	return pubIDToHex(publicID)
}

// resolveCursor turns a cursor back into the ordering pair it names. A cursor
// naming a photo from another calendar is refused rather than silently paging
// through this one from an unrelated position.
func resolveCursor(ctx context.Context, deps Deps, calendarID uint32, cursor string) (time.Time, uint32, error) {
	pub, err := parseUUID(cursor)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	photo, err := deps.Queries.GetAlbumPhotoByPublicID(ctx, pub)
	if err != nil || photo.CalendarID != calendarID {
		return time.Time{}, 0, fmt.Errorf("invalid cursor")
	}
	return photo.TakenAt, photo.ID, nil
}

// photoETag names one revision of a photo by its last write.
func photoETag(updatedAt sql.NullTime, createdAt time.Time) string {
	stamp := createdAt
	if updatedAt.Valid {
		stamp = updatedAt.Time
	}
	return `"` + stamp.UTC().Format("20060102T150405.000") + `"`
}

// checkIfMatch compares the caller's If-Match with the photo's current tag.
func checkIfMatch(ifMatch precondition.IfMatch, p generated.AlbumPhoto) error {
	switch err := ifMatch.Check(photoETag(p.UpdatedAt, p.CreatedAt)); {
	case err == nil:
		return nil
	case errors.Is(err, precondition.ErrStale):
		return apierrors.AlbumStale
	default:
		return apierrors.BadIfMatch
	}
}

// visibleEventLinks returns the public ids of the given events the reader may
// see, keyed by internal id. An event the reader cannot see is absent, so a
// photo linked to it answers as if it had no link.
func visibleEventLinks(ctx context.Context, q *generated.Queries, cal generated.Calendar, userID uint32, eventIDs []uint32) (map[uint32][]byte, error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}
	events, err := q.ListAlbumPhotoEvents(ctx, eventIDs)
	if err != nil {
		return nil, err
	}
	nullable := make([]sql.NullInt32, len(eventIDs))
	for i, id := range eventIDs {
		nullable[i] = nullInt32(id)
	}
	rows, err := q.ListAlbumPhotoEventAttendees(ctx, nullable)
	if err != nil {
		return nil, err
	}
	attendees := map[uint32][]eventacl.Attendee{}
	for _, r := range rows {
		id := uint32(r.EventID.Int32)
		attendees[id] = append(attendees[id], eventacl.Attendee{UserID: r.UserID, Enabled: true})
	}
	out := make(map[uint32][]byte, len(events))
	for _, e := range events {
		d := eventacl.Decide(eventacl.Event{
			Visibility:      eventacl.Visibility(e.Visibility),
			CalendarDefault: eventacl.Visibility(cal.DefaultEventVisibility),
			OwnerUserID:     e.OwnerUserID,
			CreatedByUserID: e.CreatedByUserID,
		}, eventacl.Reader{UserID: userID, IsMember: true}, eventacl.OccurrenceAttendees(attendees[e.ID], nil))
		if d.Exists {
			out[e.ID] = e.PublicID
		}
	}
	return out, nil
}

// eventLink is the eventId a photo answers with: null without a link, and null
// when the reader may not see the linked event.
func eventLink(links map[uint32][]byte, eventID sql.NullInt32) *string {
	if !eventID.Valid {
		return nil
	}
	pub, ok := links[uint32(eventID.Int32)]
	if !ok {
		return nil
	}
	s := pubIDToHex(pub)
	return &s
}

// setUploader fills the uploader fields, leaving them empty when the user is gone.
func setUploader(ctx context.Context, deps Deps, resp *AlbumPhotoResponse, userID uint32) {
	u, err := deps.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return
	}
	resp.UploaderID = pubIDToHex(u.PublicID)
	resp.UploaderName = u.DisplayName
	resp.UploaderAvatarURL = avatars.New(deps.Queries, deps.Storage).ForUser(ctx, u)
}

type albumPhotoListRow struct {
	id              uint32
	publicID        []byte
	calendarEventID sql.NullInt32
	caption         string
	contentType     string
	byteSize        uint64
	width           sql.NullInt32
	height          sql.NullInt32
	// storageKey is the listing's resolved key: the photo's storage object
	// when it has one, its own key until the backfill reaches it.
	storageKey string
	// thumbnailStorageKey is the grid-sized rendering's key, absent when the
	// photo has no thumbnail.
	thumbnailStorageKey      sql.NullString
	takenAt                  time.Time
	createdAt                time.Time
	updatedAt                sql.NullTime
	uploaderPublicID         []byte
	uploaderName             string
	uploaderAvatarURL        sql.NullString
	uploaderAvatarStorageKey sql.NullString
}

func firstPagePhotoRows(rows []generated.ListAlbumPhotosFirstPageRow) []albumPhotoListRow {
	out := make([]albumPhotoListRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, albumPhotoListRow{
			id:                       r.ID,
			publicID:                 r.PublicID,
			calendarEventID:          r.CalendarEventID,
			caption:                  r.Caption,
			contentType:              r.ContentType,
			byteSize:                 r.ByteSize,
			width:                    r.Width,
			height:                   r.Height,
			storageKey:               r.ImageStorageKey,
			thumbnailStorageKey:      r.ThumbnailStorageKey,
			takenAt:                  r.TakenAt,
			createdAt:                r.CreatedAt,
			updatedAt:                r.UpdatedAt,
			uploaderPublicID:         r.UploaderPublicID,
			uploaderName:             r.UploaderDisplayName,
			uploaderAvatarURL:        r.UploaderAvatarURL,
			uploaderAvatarStorageKey: r.UploaderAvatarStorageKey,
		})
	}
	return out
}

func afterPagePhotoRows(rows []generated.ListAlbumPhotosAfterRow) []albumPhotoListRow {
	out := make([]albumPhotoListRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, albumPhotoListRow{
			id:                       r.ID,
			publicID:                 r.PublicID,
			calendarEventID:          r.CalendarEventID,
			caption:                  r.Caption,
			contentType:              r.ContentType,
			byteSize:                 r.ByteSize,
			width:                    r.Width,
			height:                   r.Height,
			storageKey:               r.ImageStorageKey,
			thumbnailStorageKey:      r.ThumbnailStorageKey,
			takenAt:                  r.TakenAt,
			createdAt:                r.CreatedAt,
			updatedAt:                r.UpdatedAt,
			uploaderPublicID:         r.UploaderPublicID,
			uploaderName:             r.UploaderDisplayName,
			uploaderAvatarURL:        r.UploaderAvatarURL,
			uploaderAvatarStorageKey: r.UploaderAvatarStorageKey,
		})
	}
	return out
}

func mapListPhoto(ctx context.Context, deps Deps, cal generated.Calendar, p albumPhotoListRow, links map[uint32][]byte, av *avatars.Resolver) AlbumPhotoResponse {
	resp := AlbumPhotoResponse{
		ID:                pubIDToHex(p.publicID),
		CalendarID:        pubIDToHex(cal.PublicID),
		Caption:           p.caption,
		ContentType:       p.contentType,
		ByteSize:          int64(p.byteSize),
		TakenAt:           p.takenAt,
		CreatedAt:         p.createdAt,
		ETag:              photoETag(p.updatedAt, p.createdAt),
		EventID:           eventLink(links, p.calendarEventID),
		UploaderID:        pubIDToHex(p.uploaderPublicID),
		UploaderName:      p.uploaderName,
		UploaderAvatarURL: av.FromKey(ctx, p.uploaderAvatarStorageKey, p.uploaderAvatarURL),
	}
	if p.width.Valid {
		w := int(p.width.Int32)
		resp.Width = &w
	}
	if p.height.Valid {
		h := int(p.height.Int32)
		resp.Height = &h
	}
	if deps.Storage != nil {
		if url, err := deps.Storage.SignedGetURL(p.storageKey, imageTTL); err == nil {
			resp.ImageURL = url
		} else {
			slog.WarnContext(ctx, "failed to presign album photo URL", "photoID", p.id, "error", err)
		}
		if p.thumbnailStorageKey.Valid {
			if url, err := deps.Storage.SignedGetURL(p.thumbnailStorageKey.String, imageTTL); err == nil {
				resp.ThumbnailURL = url
			} else {
				slog.WarnContext(ctx, "failed to presign album thumbnail URL", "photoID", p.id, "error", err)
			}
		}
	}
	return resp
}

func mapPhoto(ctx context.Context, deps Deps, cal generated.Calendar, userID uint32, p generated.AlbumPhoto) AlbumPhotoResponse {
	var links map[uint32][]byte
	if p.CalendarEventID.Valid {
		var err error
		links, err = visibleEventLinks(ctx, deps.Queries, cal, userID, []uint32{uint32(p.CalendarEventID.Int32)})
		if err != nil {
			// The link is withheld rather than guessed at.
			slog.WarnContext(ctx, "failed to resolve album photo event link", "photoID", p.ID, "error", err)
		}
	}
	resp := AlbumPhotoResponse{
		ID:          pubIDToHex(p.PublicID),
		CalendarID:  pubIDToHex(cal.PublicID),
		Caption:     p.Caption,
		ContentType: p.ContentType,
		ByteSize:    int64(p.ByteSize),
		TakenAt:     p.TakenAt,
		CreatedAt:   p.CreatedAt,
		ETag:        photoETag(p.UpdatedAt, p.CreatedAt),
		EventID:     eventLink(links, p.CalendarEventID),
	}
	setUploader(ctx, deps, &resp, p.UploadedByUserID)
	if p.Width.Valid {
		w := int(p.Width.Int32)
		resp.Width = &w
	}
	if p.Height.Valid {
		h := int(p.Height.Int32)
		resp.Height = &h
	}
	if deps.Storage != nil {
		if url, err := deps.Storage.SignedGetURL(photoObjectKey(ctx, deps, p), imageTTL); err == nil {
			resp.ImageURL = url
		} else {
			slog.WarnContext(ctx, "failed to presign album photo URL", "photoID", p.ID, "error", err)
		}
		if key := thumbnailObjectKey(ctx, deps, p); key != "" {
			if url, err := deps.Storage.SignedGetURL(key, imageTTL); err == nil {
				resp.ThumbnailURL = url
			} else {
				slog.WarnContext(ctx, "failed to presign album thumbnail URL", "photoID", p.ID, "error", err)
			}
		}
	}
	return resp
}

// resolveEventForCalendar parses the event public ID and returns its internal
// numeric ID, or nil if the event does not belong to the calendar or the
// reader may not see it.
func resolveEventForCalendar(ctx context.Context, deps Deps, cal generated.Calendar, userID uint32, eventPubID string) (sql.NullInt32, error) {
	if eventPubID == "" {
		return sql.NullInt32{}, nil
	}
	pub, err := parseUUID(eventPubID)
	if err != nil {
		return sql.NullInt32{}, apierrors.EventNotFound
	}
	evt, err := deps.Queries.GetCalendarEventByPublicID(ctx, generated.GetCalendarEventByPublicIDParams{
		WorkspaceID: deps.WorkspaceID,
		PublicID:    pub,
	})
	if err != nil || evt.CalendarID != cal.ID {
		return sql.NullInt32{}, apierrors.EventNotFound
	}
	links, err := visibleEventLinks(ctx, deps.Queries, cal, userID, []uint32{evt.ID})
	if err != nil {
		return sql.NullInt32{}, err
	}
	if _, ok := links[evt.ID]; !ok {
		return sql.NullInt32{}, apierrors.EventNotFound
	}
	return sql.NullInt32{Int32: int32(evt.ID), Valid: true}, nil
}

// ListPhotos returns a page of photos in a calendar's album, newest first.
func ListPhotos(deps Deps) func(context.Context, *ListPhotosInput) (*ListPhotosOutput, error) {
	return func(ctx context.Context, in *ListPhotosInput) (*ListPhotosOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		limit := int32(in.Limit)
		if limit <= 0 {
			limit = defaultPageSize
		}
		if limit > maxPageSize {
			limit = maxPageSize
		}
		// fetch one extra to determine if there is a next page
		fetchLimit := limit + 1

		var photos []albumPhotoListRow
		if in.Cursor == "" {
			rows, qerr := deps.Queries.ListAlbumPhotosFirstPage(ctx, generated.ListAlbumPhotosFirstPageParams{
				CalendarID: cal.ID,
				Limit:      fetchLimit,
			})
			err = qerr
			photos = firstPagePhotoRows(rows)
		} else {
			takenAt, idBefore, derr := resolveCursor(ctx, deps, cal.ID, in.Cursor)
			if derr != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			rows, qerr := deps.Queries.ListAlbumPhotosAfter(ctx, generated.ListAlbumPhotosAfterParams{
				CalendarID:  cal.ID,
				TakenBefore: takenAt,
				IDBefore:    idBefore,
				Limit:       fetchLimit,
			})
			err = qerr
			photos = afterPagePhotoRows(rows)
		}
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		out := &ListPhotosOutput{}
		out.Body.Items = make([]AlbumPhotoResponse, 0, len(photos))

		hasMore := int32(len(photos)) > limit
		if hasMore {
			photos = photos[:limit]
		}

		var eventIDs []uint32
		for _, p := range photos {
			if p.calendarEventID.Valid {
				eventIDs = append(eventIDs, uint32(p.calendarEventID.Int32))
			}
		}
		links, err := visibleEventLinks(ctx, deps.Queries, cal, userID, eventIDs)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		av := avatars.New(deps.Queries, deps.Storage)
		for _, p := range photos {
			out.Body.Items = append(out.Body.Items, mapListPhoto(ctx, deps, cal, p, links, av))
		}
		if hasMore && len(photos) > 0 {
			last := photos[len(photos)-1]
			out.Body.NextCursor = encodeCursor(last.publicID)
		}
		return out, nil
	}
}

// declaresThumbnail reports whether the body is asking for a second upload.
//
// A thumbnail is declared by all of its fields or by none. Half a
// declaration is a client that believes it is sending one, and answering that
// with no URL and no error leaves the mistake invisible until somebody notices
// the grid is downloading full-size pictures.
func declaresThumbnail(b PresignPhotoBody) bool {
	return b.ThumbnailContentType != "" || b.ThumbnailByteSize > 0 || b.ThumbnailSHA256 != ""
}

// validateThumbnailDeclaration applies the photo's own rules to the smaller
// rendering: it is an image of a type a browser renders inert, and it is
// thumbnail-sized. Returns nil when nothing was declared, which is normal.
func validateThumbnailDeclaration(b PresignPhotoBody) error {
	if !declaresThumbnail(b) {
		return nil
	}
	if b.ThumbnailContentType == "" || b.ThumbnailByteSize <= 0 || b.ThumbnailSHA256 == "" {
		return apierrors.AlbumThumbnailIncomplete
	}
	if !isImageContentType(b.ThumbnailContentType) {
		return apierrors.AlbumInvalidContentType
	}
	if b.ThumbnailByteSize > maxThumbnailSize {
		return apierrors.AlbumThumbnailTooLarge
	}
	return nil
}

// uploadKey says where the caller must send an object's bytes. An object not
// yet verified is uploaded to its own key, which is the key later reservations
// of the same digest also wait on. A verified one already has its bytes; a
// caller who must still prove possession sends a copy to ownKey, which the
// confirm reads back and then discards.
func uploadKey(object generated.StorageObject, ownKey string) string {
	if blobref.Verified(object) {
		return ownKey
	}
	return object.StorageKey
}

// PresignUpload issues a presigned PUT URL for adding a photo to the album,
// and a second one for its thumbnail when the request declares one.
// The row is created disabled and stays out of every listing until
// ConfirmPhoto finds the upload; a presign that is never followed by a PUT is
// removed by the abandoned-upload sweep.
//
// The declared digests reserve the storage objects the photo will point at,
// and each reservation counts as a reference from here on; the sweep gives it
// back with the row.
func PresignUpload(deps Deps) func(context.Context, *PresignPhotoInput) (*PresignPhotoOutput, error) {
	return func(ctx context.Context, in *PresignPhotoInput) (*PresignPhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		if !isImageContentType(in.Body.ContentType) {
			return nil, apierrors.ToHuma(apierrors.AlbumInvalidContentType)
		}
		if in.Body.ByteSize > maxPhotoSize {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoTooLarge)
		}
		if err := validateThumbnailDeclaration(in.Body); err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		digest, err := blobref.ParseDigest(in.Body.SHA256)
		if err != nil {
			return nil, digestError(ctx, err)
		}
		// A thumbnail identical to the photo is the photo: it is drawn from the
		// picture, and a second reservation of the same bytes would wait on the
		// same key twice.
		withThumbnail := declaresThumbnail(in.Body)
		var thumbDigest []byte
		if withThumbnail {
			if thumbDigest, err = blobref.ParseDigest(in.Body.ThumbnailSHA256); err != nil {
				return nil, digestError(ctx, err)
			}
			withThumbnail = !bytes.Equal(thumbDigest, digest)
		}
		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		eventID, err := resolveEventForCalendar(ctx, deps, cal, userID, in.Body.EventID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		photoPubID, _ := uuid.NewV7()
		calPubHex := pubIDToHex(cal.PublicID)
		photoPubHex := photoPubID.String()
		key := photoStorageKey(calPubHex, photoPubHex)
		thumbKey := albumblob.ThumbnailKey(key)

		takenAt := in.Body.TakenAt
		if takenAt.IsZero() {
			takenAt = time.Now().UTC()
		}

		width := sql.NullInt32{}
		if in.Body.Width > 0 {
			width = sql.NullInt32{Int32: int32(in.Body.Width), Valid: true}
		}
		height := sql.NullInt32{}
		if in.Body.Height > 0 {
			height = sql.NullInt32{Int32: int32(in.Body.Height), Valid: true}
		}

		contentType := strings.ToLower(in.Body.ContentType)
		var photoObject, thumbObject generated.StorageObject
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			var err error
			photoObject, err = blobref.Reserve(ctx, q, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      digest,
				ByteSize:    uint64(in.Body.ByteSize),
				ContentType: contentType,
				StorageKey:  key,
			})
			if err != nil {
				return err
			}
			var thumbObjectID sql.NullInt32
			if withThumbnail {
				thumbObject, err = blobref.Reserve(ctx, q, blobref.Blob{
					WorkspaceID: deps.WorkspaceID,
					SHA256:      thumbDigest,
					ByteSize:    uint64(in.Body.ThumbnailByteSize),
					ContentType: strings.ToLower(in.Body.ThumbnailContentType),
					StorageKey:  thumbKey,
				})
				if err != nil {
					return err
				}
				thumbObjectID = nullInt32(thumbObject.ID)
			}
			_, err = q.CreateAlbumPhoto(ctx, generated.CreateAlbumPhotoParams{
				PublicID:          photoPubID[:],
				WorkspaceID:       deps.WorkspaceID,
				CalendarID:        cal.ID,
				UploadedByUserID:  userID,
				CalendarEventID:   eventID,
				Caption:           in.Body.Caption,
				ContentType:       contentType,
				ByteSize:          uint64(in.Body.ByteSize),
				Width:             width,
				Height:            height,
				StorageKey:        key,
				StorageObjectID:   nullInt32(photoObject.ID),
				ThumbnailObjectID: thumbObjectID,
				TakenAt:           takenAt,
			})
			return err
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		url, err := deps.Storage.SignedPutURL(uploadKey(photoObject, key), in.Body.ContentType, in.Body.ByteSize, uploadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &PresignPhotoOutput{}
		out.Body.PhotoID = photoPubHex
		out.Body.UploadURL = url
		if withThumbnail {
			// A thumbnail URL that cannot be signed is left out rather than
			// failing the request. The picture's URL is already in hand, and
			// refusing it here would mean losing a photo over a rendering
			// nothing needs -- the same rule the confirm applies when the
			// thumbnail does not arrive. The caller sees the field absent,
			// which it already has to handle.
			thumbURL, terr := deps.Storage.SignedPutURL(uploadKey(thumbObject, thumbKey),
				in.Body.ThumbnailContentType, in.Body.ThumbnailByteSize, uploadTTL)
			if terr != nil {
				slog.WarnContext(ctx, "failed to presign album thumbnail upload", "key", key, "error", terr)
			} else {
				out.Body.ThumbnailUploadURL = thumbURL
			}
		}
		return out, nil
	}
}

func nullInt32(v uint32) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(v), Valid: true}
}

// digestError maps a refused declared digest or read-back to its Spec.
func digestError(ctx context.Context, err error) error {
	if spec, ok := apierrors.FromBlobref(err); ok {
		return apierrors.ToHuma(spec)
	}
	return apierrors.Internal(ctx, err)
}

// uploadedBytes locates the bytes an upload left for a reserved object: at the
// photo's own key when the caller sent them there, else at the object's key.
// sent is true in the first case.
func uploadedBytes(ctx context.Context, deps Deps, object generated.StorageObject, ownKey string) (key string, info storage.ObjectInfo, sent, exists bool, err error) {
	info, sent, err = deps.Storage.StatObject(ctx, ownKey)
	if err != nil {
		return "", storage.ObjectInfo{}, false, false, err
	}
	if sent {
		return ownKey, info, true, true, nil
	}
	info, exists, err = deps.Storage.StatObject(ctx, object.StorageKey)
	return object.StorageKey, info, false, exists, err
}

// readBack checks the bytes at key against the digest the reservation holds.
func readBack(ctx context.Context, deps Deps, key string, digest []byte, limit int64) error {
	obj, exists, err := deps.Storage.Open(ctx, key)
	if err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	defer obj.Close()
	return blobref.VerifyReadBack(obj, digest, limit)
}

// ConfirmPhoto finalizes a presigned album upload by reading the stored bytes
// back against the digest declared at presign, then enabling the row so it
// becomes visible. Rows whose upload is abandoned stay disabled and never
// surface, so a presign that is never followed by a PUT leaves no broken photo
// in the album.
func ConfirmPhoto(deps Deps) func(context.Context, *ConfirmPhotoInput) (*ConfirmPhotoOutput, error) {
	return func(ctx context.Context, in *ConfirmPhotoInput) (*ConfirmPhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		pub, err := parseUUID(in.PhotoID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoNotFound)
		}
		p, err := deps.Queries.GetAlbumPhotoByPublicID(ctx, pub)
		if err != nil || p.CalendarID != cal.ID {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoNotFound)
		}
		if p.Enabled || p.RetiredAt.Valid || p.UploadedByUserID != userID || !p.StorageObjectID.Valid {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoNotFound)
		}
		object, err := deps.Queries.GetStorageObjectByID(ctx, uint32(p.StorageObjectID.Int32))
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}

		key, info, sent, exists, err := uploadedBytes(ctx, deps, object, p.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !exists {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoNotFound)
		}
		if info.Size > maxPhotoSize {
			deleteMismatchedPhoto(ctx, deps, p, key)
			return nil, apierrors.ToHuma(apierrors.UploadTooLarge)
		}
		if uint64(info.Size) != p.ByteSize {
			deleteMismatchedPhoto(ctx, deps, p, key)
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}
		// The digest decided which object these bytes are stored as, so it is
		// checked against them rather than taken on trust: otherwise an upload
		// could put anything at a key it names, and every later photo of the
		// real file would resolve to it.
		if err := readBack(ctx, deps, key, object.Sha256, maxPhotoSize); err != nil {
			if spec, ok := apierrors.FromBlobref(err); ok {
				deleteMismatchedPhoto(ctx, deps, p, key)
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		var queued []string
		// Logged on Confirm, not on the earlier presign: a presign whose
		// upload is abandoned never becomes a real photo, so it must not
		// appear in the feed.
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			queued = nil
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			// Verified bytes at the object's key are somebody's stored file:
			// confirming against them without having sent them takes being able
			// to read them already.
			if !sent && blobref.Verified(object) {
				readable, err := q.CanReadObject(ctx, generated.CanReadObjectParams{
					UserID:        userID,
					ObjectID:      object.ID,
					PhotoObjectID: p.StorageObjectID,
				})
				if err != nil {
					return err
				}
				if !readable.Valid || !readable.Bool {
					return apierrors.AlbumPhotoNotFound
				}
			}
			res, err := q.ConfirmAlbumPhoto(ctx, generated.ConfirmAlbumPhotoParams{
				ID:               p.ID,
				UploadedByUserID: userID,
			})
			if err != nil {
				return err
			}
			// The update re-checks enabled = FALSE, so a repeated confirm
			// affects nothing and does not append a second event for one photo.
			if confirmed, err := blobref.ChangedOne(res); err != nil {
				return err
			} else if !confirmed {
				return apierrors.AlbumPhotoNotFound
			}
			verified, err := blobref.Store(ctx, q, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      object.Sha256,
				ByteSize:    uint64(info.Size),
				ContentType: p.ContentType,
				StorageKey:  key,
			})
			if err != nil {
				return err
			}
			// A copy sent alongside bytes another upload verified first is held
			// by nothing.
			if sent && verified.StorageKey != key {
				if err := blobref.QueueRemoval(ctx, q, key); err != nil {
					return err
				}
				queued = append(queued, key)
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypePhotoUploaded,
				Summary:     p.Caption,
				Subject:     p.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		if err := blobref.RemoveQueued(ctx, deps.Queries, deps.Storage, queued...); err != nil {
			slog.WarnContext(ctx, "failed to remove superseded album bytes; left to the drainer", "photoID", p.ID, "error", err)
		}
		// The thumbnail is settled after that commit, in a transaction of its
		// own, and nothing it can do reaches back. A photo whose bytes arrived
		// is never lost to a second, optional upload that did not: the picture
		// is already confirmed by the time this runs, and every way it can fail
		// leaves the photo exactly as it is, drawn from the picture itself.
		settleThumbnail(ctx, deps, p)
		// Re-read rather than patching the copy in hand: the response presigns
		// whatever the photo now points at.
		confirmed, err := deps.Queries.GetAlbumPhotoByPublicID(ctx, p.PublicID)
		if err != nil {
			return nil, apierrors.Internal(ctx, err)
		}
		return &ConfirmPhotoOutput{Body: mapPhoto(ctx, deps, cal, userID, confirmed)}, nil
	}
}

// settleThumbnail verifies the photo's reserved grid-sized rendering, or drops
// the reservation.
//
// Nothing here reports a failure to the caller, and that is the point rather
// than an oversight: the thumbnail is optional at every step. It may not be
// there at all -- the client declared one and did not send it -- and it may be
// there and refused. Each of those leaves a confirmed photo with no thumbnail,
// which is a state every read already falls back from. The alternative,
// failing the confirm, would answer "your picture is stored" with an error and
// lose the photo to a rendering nothing needs.
func settleThumbnail(ctx context.Context, deps Deps, p generated.AlbumPhoto) {
	if !p.ThumbnailObjectID.Valid {
		return
	}
	objectID := uint32(p.ThumbnailObjectID.Int32)
	object, err := deps.Queries.GetStorageObjectByID(ctx, objectID)
	if err != nil {
		slog.WarnContext(ctx, "failed to read album thumbnail object", "photoID", p.ID, "error", err)
		return
	}
	ownKey := albumblob.ThumbnailKey(p.StorageKey)
	key, info, sent, exists, err := uploadedBytes(ctx, deps, object, ownKey)
	if err != nil {
		slog.WarnContext(ctx, "failed to stat album thumbnail object", "photoID", p.ID, "key", key, "error", err)
		return
	}

	usable := exists
	switch {
	case !exists:
	case info.Size <= 0 || info.Size > maxThumbnailSize:
		slog.WarnContext(ctx, "album thumbnail object is not thumbnail-sized, leaving the photo without one",
			"photoID", p.ID, "size", info.Size)
		usable = false
	case !isImageContentType(info.ContentType):
		slog.WarnContext(ctx, "album thumbnail object is not a renderable image, leaving the photo without one",
			"photoID", p.ID, "contentType", info.ContentType)
		usable = false
	}
	if usable {
		if err := readBack(ctx, deps, key, object.Sha256, maxThumbnailSize); err != nil {
			slog.WarnContext(ctx, "album thumbnail does not match its declared digest, leaving the photo without one",
				"photoID", p.ID, "error", err)
			usable = false
		}
	}

	var queued []string
	err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		queued = nil
		if err := calresolve.LockShared(ctx, q, p.CalendarID); err != nil {
			return err
		}
		keep := usable
		if keep && !sent && blobref.Verified(object) {
			readable, err := q.CanReadObject(ctx, generated.CanReadObjectParams{
				UserID:        p.UploadedByUserID,
				ObjectID:      object.ID,
				PhotoObjectID: p.ThumbnailObjectID,
			})
			if err != nil {
				return err
			}
			keep = readable.Valid && readable.Bool
		}
		if keep {
			verified, err := blobref.Store(ctx, q, blobref.Blob{
				WorkspaceID: deps.WorkspaceID,
				SHA256:      object.Sha256,
				ByteSize:    uint64(info.Size),
				ContentType: strings.ToLower(info.ContentType),
				StorageKey:  key,
			})
			if err != nil {
				return err
			}
			if sent && verified.StorageKey != key {
				queued = append(queued, key)
			}
		} else {
			res, err := q.ClearAlbumPhotoThumbnail(ctx, generated.ClearAlbumPhotoThumbnailParams{
				ID:                p.ID,
				ThumbnailObjectID: p.ThumbnailObjectID,
			})
			if err != nil {
				return err
			}
			if cleared, err := blobref.ChangedOne(res); err != nil {
				return err
			} else if cleared {
				if err := blobref.Release(ctx, q, objectID); err != nil {
					return err
				}
			}
			// Bytes sent to the photo's own key, or left at a key no verified
			// object claims, can only be what failed the check.
			if exists && (sent || !blobref.Verified(object)) {
				queued = append(queued, key)
			}
		}
		return blobref.QueueRemoval(ctx, q, queued...)
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to settle album thumbnail object", "photoID", p.ID, "error", err)
		return
	}
	if err := blobref.RemoveQueued(ctx, deps.Queries, deps.Storage, queued...); err != nil {
		slog.WarnContext(ctx, "failed to remove album thumbnail bytes; left to the drainer", "photoID", p.ID, "error", err)
	}
}

// deleteMismatchedPhoto removes the pending row of an upload that failed the
// Confirm-time checks, gives back the references its reservations took, queues
// the offending bytes for removal in the same transaction and removes them
// right after, so a mismatched upload does not linger as an orphan until the
// abandoned-upload sweep. Best-effort: errors are logged, not surfaced, since
// the caller is already returning the mismatch error to the client.
//
// key is where the offending bytes are. The drainer never removes a key a
// verified object claims, so queueing a key that turns out to be shared is
// harmless.
func deleteMismatchedPhoto(ctx context.Context, deps Deps, p generated.AlbumPhoto, key string) {
	var queued []string
	if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		queued = nil
		res, err := q.DeletePendingAlbumPhoto(ctx, generated.DeletePendingAlbumPhotoParams{
			ID:               p.ID,
			UploadedByUserID: p.UploadedByUserID,
		})
		if err != nil {
			return err
		}
		if deleted, err := blobref.ChangedOne(res); err != nil || !deleted {
			return err
		}
		for _, id := range []sql.NullInt32{p.StorageObjectID, p.ThumbnailObjectID} {
			if !id.Valid {
				continue
			}
			if err := blobref.Release(ctx, q, uint32(id.Int32)); err != nil {
				return err
			}
		}
		queued = []string{key}
		return blobref.QueueRemoval(ctx, q, queued...)
	}); err != nil {
		slog.WarnContext(ctx, "failed to discard mismatched album photo", "photoID", p.ID, "error", err)
		return
	}
	if err := blobref.RemoveQueued(ctx, deps.Queries, deps.Storage, queued...); err != nil {
		slog.WarnContext(ctx, "failed to remove mismatched album photo object; left to the drainer", "key", key, "error", err)
	}
}

func loadPhotoForCalendar(ctx context.Context, deps Deps, calID uint32, photoPubID string) (generated.AlbumPhoto, error) {
	pub, err := parseUUID(photoPubID)
	if err != nil {
		return generated.AlbumPhoto{}, apierrors.AlbumPhotoNotFound
	}
	p, err := deps.Queries.GetAlbumPhotoByPublicID(ctx, pub)
	if err != nil {
		return generated.AlbumPhoto{}, apierrors.AlbumPhotoNotFound
	}
	if p.CalendarID != calID || !p.Enabled {
		return generated.AlbumPhoto{}, apierrors.AlbumPhotoNotFound
	}
	return p, nil
}

// lockPhoto reads a photo for a write, under its row lock, inside the
// transaction that commits the write. A photo deleted in the meantime, or in
// another calendar, is not found.
func lockPhoto(ctx context.Context, q *generated.Queries, calID uint32, photoPubID string) (generated.AlbumPhoto, error) {
	pub, err := parseUUID(photoPubID)
	if err != nil {
		return generated.AlbumPhoto{}, apierrors.AlbumPhotoNotFound
	}
	p, err := q.GetAlbumPhotoForUpdate(ctx, generated.GetAlbumPhotoForUpdateParams{
		PublicID:   pub,
		CalendarID: calID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return generated.AlbumPhoto{}, apierrors.AlbumPhotoNotFound
	}
	return p, err
}

// UpdatePhoto edits the caption and/or event link of a photo.
func UpdatePhoto(deps Deps) func(context.Context, *UpdatePhotoInput) (*UpdatePhotoOutput, error) {
	return func(ctx context.Context, in *UpdatePhotoInput) (*UpdatePhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		var linkedEvent sql.NullInt32
		if v, ok := in.Body.EventID.Get(); ok {
			linkedEvent, err = resolveEventForCalendar(ctx, deps, cal, userID, v)
			if err != nil {
				return nil, apierrors.Respond(ctx, err)
			}
		}

		var refreshed generated.AlbumPhoto
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			photo, err := lockPhoto(ctx, q, cal.ID, in.PhotoID)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, photo); err != nil {
				return err
			}

			// Omitted members keep the values read under the lock, so a
			// concurrent edit of the other field is kept.
			caption := photo.Caption
			if v, ok := in.Body.Caption.Get(); ok {
				caption = v
			} else if in.Body.Caption.IsNull() {
				caption = ""
			}
			eventID := photo.CalendarEventID
			if in.Body.EventID.Present() {
				eventID = linkedEvent
			}

			if err := q.UpdateAlbumPhotoMeta(ctx, generated.UpdateAlbumPhotoMetaParams{
				Caption:         caption,
				CalendarEventID: eventID,
				ID:              photo.ID,
			}); err != nil {
				return err
			}
			refreshed, err = q.GetAlbumPhotoByPublicID(ctx, photo.PublicID)
			if err != nil {
				return err
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypePhotoUpdated,
				Summary:     refreshed.Caption,
				Subject:     refreshed.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		return &UpdatePhotoOutput{ETag: photoETag(refreshed.UpdatedAt, refreshed.CreatedAt), Body: mapPhoto(ctx, deps, cal, userID, refreshed)}, nil
	}
}

// DeletePhoto soft-deletes a photo and removes the underlying object best-effort.
func DeletePhoto(deps Deps) func(context.Context, *DeletePhotoInput) (*DeletePhotoOutput, error) {
	return func(ctx context.Context, in *DeletePhotoInput) (*DeletePhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if _, err := calresolve.LockWrite(ctx, q, cal.ID, userID); err != nil {
				return err
			}
			photo, err := lockPhoto(ctx, q, cal.ID, in.PhotoID)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, photo); err != nil {
				return err
			}
			// The log row is appended only for a delete this transaction made.
			res, err := q.SoftDeleteAlbumPhoto(ctx, photo.ID)
			if err != nil {
				return err
			}
			if changed, err := blobref.ChangedOne(res); err != nil {
				return err
			} else if !changed {
				return apierrors.AlbumPhotoNotFound
			}
			return eventlog.Append(ctx, q, eventlog.Entry{
				WorkspaceID: deps.WorkspaceID,
				CalendarID:  cal.ID,
				ActorUserID: userID,
				Type:        eventlog.TypePhotoDeleted,
				Summary:     photo.Caption,
				Subject:     photo.PublicID,
			})
		})
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}
		// The bytes are not deleted here, and that is not tidiness deferred:
		// photos with identical content share one storage object, so the key
		// this photo resolves to can be where another photo's picture lives.
		// Deleting it would blank that one.
		//
		// The photo is gone as access the moment the row is disabled -- every
		// path that signs a URL for one requires it to be enabled, so no new
		// URL is issued after this returns. What waits is only the reclaiming
		// of the bytes: the sweep releases the reference when it removes the
		// row, and collects the object once nothing else holds it.
		return &DeletePhotoOutput{}, nil
	}
}

// GetDownload issues a presigned GET URL for a single photo.
func GetDownload(deps Deps) func(context.Context, *DownloadPhotoInput) (*DownloadPhotoOutput, error) {
	return func(ctx context.Context, in *DownloadPhotoInput) (*DownloadPhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			return nil, apierrors.Respond(ctx, err)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		url, err := deps.Storage.SignedDownloadURL(photoObjectKey(ctx, deps, photo), photoDownloadFilename(photo), downloadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		out := &DownloadPhotoOutput{}
		out.Body.DownloadURL = url
		return out, nil
	}
}
