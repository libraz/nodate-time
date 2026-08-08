package albums

import (
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
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	"github.com/libraz/nodate-time/apps/api/internal/dbtx"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/eventlog"
	"github.com/libraz/nodate-time/apps/api/internal/http/avatars"
	"github.com/libraz/nodate-time/apps/api/internal/http/calresolve"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
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

func toAPIError(err error) error {
	var spec *apierrors.Spec
	if errors.As(err, &spec) {
		return apierrors.ToHuma(spec)
	}
	return apierrors.ToHuma(apierrors.InternalUnexpected)
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

func eventPubIDForResponse(ctx context.Context, deps Deps, evtID sql.NullInt32) string {
	if !evtID.Valid {
		return ""
	}
	evt, err := deps.Queries.GetCalendarEventByID(ctx, uint32(evtID.Int32))
	if err != nil {
		return ""
	}
	return pubIDToHex(evt.PublicID)
}

func uploaderForResponse(ctx context.Context, deps Deps, userID uint32) AlbumUploader {
	u, err := deps.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return AlbumUploader{ID: ""}
	}
	return AlbumUploader{
		ID:        pubIDToHex(u.PublicID),
		Name:      u.DisplayName,
		AvatarURL: avatars.New(deps.Queries, deps.Storage).ForUser(ctx, u),
	}
}

type albumPhotoListRow struct {
	id          uint32
	publicID    []byte
	caption     string
	contentType string
	byteSize    uint64
	width       sql.NullInt32
	height      sql.NullInt32
	// storageKey is the listing's resolved key: the photo's storage object
	// when it has one, its own key until the backfill reaches it.
	storageKey string
	// thumbnailStorageKey is the grid-sized rendering's key, absent when the
	// photo has no thumbnail.
	thumbnailStorageKey      sql.NullString
	takenAt                  time.Time
	createdAt                time.Time
	uploaderPublicID         []byte
	uploaderName             string
	uploaderAvatarURL        sql.NullString
	uploaderAvatarStorageKey sql.NullString
	eventPublicID            sql.NullString
}

func firstPagePhotoRows(rows []generated.ListAlbumPhotosFirstPageRow) []albumPhotoListRow {
	out := make([]albumPhotoListRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, albumPhotoListRow{
			id:                       r.ID,
			publicID:                 r.PublicID,
			caption:                  r.Caption,
			contentType:              r.ContentType,
			byteSize:                 r.ByteSize,
			width:                    r.Width,
			height:                   r.Height,
			storageKey:               r.ImageStorageKey,
			thumbnailStorageKey:      r.ThumbnailStorageKey,
			takenAt:                  r.TakenAt,
			createdAt:                r.CreatedAt,
			uploaderPublicID:         r.UploaderPublicID,
			uploaderName:             r.UploaderDisplayName,
			uploaderAvatarURL:        r.UploaderAvatarURL,
			uploaderAvatarStorageKey: r.UploaderAvatarStorageKey,
			eventPublicID:            r.EventPublicID,
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
			caption:                  r.Caption,
			contentType:              r.ContentType,
			byteSize:                 r.ByteSize,
			width:                    r.Width,
			height:                   r.Height,
			storageKey:               r.ImageStorageKey,
			thumbnailStorageKey:      r.ThumbnailStorageKey,
			takenAt:                  r.TakenAt,
			createdAt:                r.CreatedAt,
			uploaderPublicID:         r.UploaderPublicID,
			uploaderName:             r.UploaderDisplayName,
			uploaderAvatarURL:        r.UploaderAvatarURL,
			uploaderAvatarStorageKey: r.UploaderAvatarStorageKey,
			eventPublicID:            r.EventPublicID,
		})
	}
	return out
}

func mapListPhoto(ctx context.Context, deps Deps, cal generated.Calendar, p albumPhotoListRow, av *avatars.Resolver) AlbumPhotoResponse {
	resp := AlbumPhotoResponse{
		ID:          pubIDToHex(p.publicID),
		CalendarID:  pubIDToHex(cal.PublicID),
		Caption:     p.caption,
		ContentType: p.contentType,
		ByteSize:    int64(p.byteSize),
		TakenAt:     p.takenAt,
		CreatedAt:   p.createdAt,
		UploadedBy: AlbumUploader{
			ID:        pubIDToHex(p.uploaderPublicID),
			Name:      p.uploaderName,
			AvatarURL: av.FromKey(ctx, p.uploaderAvatarStorageKey, p.uploaderAvatarURL),
		},
	}
	if p.eventPublicID.Valid {
		resp.EventID = pubIDToHex([]byte(p.eventPublicID.String))
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
		if url, err := deps.Storage.PresignGet(ctx, p.storageKey, imageTTL); err == nil {
			resp.ImageURL = url
		} else {
			slog.WarnContext(ctx, "failed to presign album photo URL", "photoID", p.id, "error", err)
		}
		if p.thumbnailStorageKey.Valid {
			if url, err := deps.Storage.PresignGet(ctx, p.thumbnailStorageKey.String, imageTTL); err == nil {
				resp.ThumbnailURL = url
			} else {
				slog.WarnContext(ctx, "failed to presign album thumbnail URL", "photoID", p.id, "error", err)
			}
		}
	}
	return resp
}

func mapPhoto(ctx context.Context, deps Deps, cal generated.Calendar, p generated.AlbumPhoto) AlbumPhotoResponse {
	resp := AlbumPhotoResponse{
		ID:          pubIDToHex(p.PublicID),
		CalendarID:  pubIDToHex(cal.PublicID),
		Caption:     p.Caption,
		ContentType: p.ContentType,
		ByteSize:    int64(p.ByteSize),
		TakenAt:     p.TakenAt,
		CreatedAt:   p.CreatedAt,
		EventID:     eventPubIDForResponse(ctx, deps, p.CalendarEventID),
		UploadedBy:  uploaderForResponse(ctx, deps, p.UploadedByUserID),
	}
	if p.Width.Valid {
		w := int(p.Width.Int32)
		resp.Width = &w
	}
	if p.Height.Valid {
		h := int(p.Height.Int32)
		resp.Height = &h
	}
	if deps.Storage != nil {
		if url, err := deps.Storage.PresignGet(ctx, photoObjectKey(ctx, deps, p), imageTTL); err == nil {
			resp.ImageURL = url
		} else {
			slog.WarnContext(ctx, "failed to presign album photo URL", "photoID", p.ID, "error", err)
		}
		if key := thumbnailObjectKey(ctx, deps, p); key != "" {
			if url, err := deps.Storage.PresignGet(ctx, key, imageTTL); err == nil {
				resp.ThumbnailURL = url
			} else {
				slog.WarnContext(ctx, "failed to presign album thumbnail URL", "photoID", p.ID, "error", err)
			}
		}
	}
	return resp
}

// resolveEventForCalendar parses the event public ID and returns its internal
// numeric ID, or nil if the event does not belong to the calendar.
func resolveEventForCalendar(ctx context.Context, deps Deps, calID uint32, eventPubID string) (sql.NullInt32, error) {
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
	if err != nil || evt.CalendarID != calID {
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
			return nil, toAPIError(err)
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
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListPhotosOutput{}
		out.Body.Items = make([]AlbumPhotoResponse, 0, len(photos))

		hasMore := int32(len(photos)) > limit
		if hasMore {
			photos = photos[:limit]
		}
		av := avatars.New(deps.Queries, deps.Storage)
		for _, p := range photos {
			out.Body.Items = append(out.Body.Items, mapListPhoto(ctx, deps, cal, p, av))
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
// A thumbnail is declared by both of its fields or by neither. Half a
// declaration is a client that believes it is sending one, and answering that
// with no URL and no error leaves the mistake invisible until somebody notices
// the grid is downloading full-size pictures.
func declaresThumbnail(b PresignPhotoBody) bool {
	return b.ThumbnailContentType != "" || b.ThumbnailByteSize > 0
}

// validateThumbnailDeclaration applies the photo's own rules to the smaller
// rendering: it is an image of a type a browser renders inert, and it is
// thumbnail-sized. Returns nil when nothing was declared, which is normal.
func validateThumbnailDeclaration(b PresignPhotoBody) error {
	if !declaresThumbnail(b) {
		return nil
	}
	if b.ThumbnailContentType == "" || b.ThumbnailByteSize <= 0 {
		return apierrors.AlbumThumbnailIncomplete
	}
	if !isImageContentType(b.ThumbnailContentType) {
		return apierrors.InvalidImageContentType
	}
	if b.ThumbnailByteSize > maxThumbnailSize {
		return apierrors.AlbumThumbnailTooLarge
	}
	return nil
}

// PresignUpload issues a presigned PUT URL for adding a photo to the album,
// and a second one for its thumbnail when the request declares one.
// The metadata row is created with the storage key in the same call; if the
// client never PUTs the object, the row will simply have no underlying object
// in MinIO and the photo will render as broken — a future janitor can clean
// these up by checking StatObject.
func PresignUpload(deps Deps) func(context.Context, *PresignPhotoInput) (*PresignPhotoOutput, error) {
	return func(ctx context.Context, in *PresignPhotoInput) (*PresignPhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}
		if !isImageContentType(in.Body.ContentType) {
			return nil, apierrors.ToHuma(apierrors.InvalidImageContentType)
		}
		if in.Body.ByteSize > maxPhotoSize {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoTooLarge)
		}
		if err := validateThumbnailDeclaration(in.Body); err != nil {
			return nil, toAPIError(err)
		}
		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		eventID, err := resolveEventForCalendar(ctx, deps, cal.ID, in.Body.EventID)
		if err != nil {
			return nil, toAPIError(err)
		}

		photoPubID, _ := uuid.NewV7()
		calPubHex := pubIDToHex(cal.PublicID)
		photoPubHex := photoPubID.String()
		key := photoStorageKey(calPubHex, photoPubHex)

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

		_, err = deps.Queries.CreateAlbumPhoto(ctx, generated.CreateAlbumPhotoParams{
			PublicID:         photoPubID[:],
			WorkspaceID:      deps.WorkspaceID,
			CalendarID:       cal.ID,
			UploadedByUserID: userID,
			CalendarEventID:  eventID,
			Caption:          in.Body.Caption,
			ContentType:      strings.ToLower(in.Body.ContentType),
			ByteSize:         uint64(in.Body.ByteSize),
			Width:            width,
			Height:           height,
			StorageKey:       key,
			TakenAt:          takenAt,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		url, err := deps.Storage.PresignPut(ctx, key, in.Body.ContentType, in.Body.ByteSize, uploadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &PresignPhotoOutput{}
		out.Body.PhotoID = photoPubHex
		out.Body.UploadURL = url
		if declaresThumbnail(in.Body) {
			// A thumbnail URL that cannot be signed is left out rather than
			// failing the request. The picture's URL is already in hand, and
			// refusing it here would mean losing a photo over a rendering
			// nothing needs -- the same rule the confirm applies when the
			// thumbnail does not arrive. The caller sees the field absent,
			// which it already has to handle.
			thumbURL, terr := deps.Storage.PresignPut(ctx, albumblob.ThumbnailKey(key),
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

// ConfirmPhoto finalizes a presigned album upload by verifying the object was
// actually stored, then enabling the row so it becomes visible. Rows whose
// upload is abandoned stay disabled and never surface, so a presign that is
// never followed by a PUT leaves no broken photo in the album.
func ConfirmPhoto(deps Deps) func(context.Context, *ConfirmPhotoInput) (*ConfirmPhotoOutput, error) {
	return func(ctx context.Context, in *ConfirmPhotoInput) (*ConfirmPhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
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
		if p.Enabled || p.UploadedByUserID != userID {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoNotFound)
		}

		info, exists, err := deps.Storage.StatObject(ctx, p.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !exists {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoNotFound)
		}
		if info.Size > maxPhotoSize {
			deleteMismatchedPhoto(ctx, deps, p)
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoTooLarge)
		}
		if uint64(info.Size) != p.ByteSize {
			deleteMismatchedPhoto(ctx, deps, p)
			return nil, apierrors.ToHuma(apierrors.BadRequest)
		}

		// The digest is read from what landed rather than declared by the
		// client, because the album's presign happens before the bytes exist:
		// there is no digest to name a key with, so it is computed here, once,
		// on the upload path. It is what puts the photo on the same footing as
		// every other blob -- one object row, one reference count, one sweep.
		digest, err := deps.Storage.SHA256(ctx, p.StorageKey)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		// Logged on Confirm, not on the earlier presign: a presign whose
		// upload is abandoned never becomes a real photo, so it must not
		// appear in the feed.
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			res, err := q.ConfirmAlbumPhoto(ctx, generated.ConfirmAlbumPhotoParams{
				ID:               p.ID,
				UploadedByUserID: userID,
			})
			if err != nil {
				return err
			}
			// The update re-checks enabled = FALSE, so a repeated confirm
			// affects nothing and does not append a second event for one photo.
			affected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return apierrors.AlbumPhotoNotFound
			}
			if err := albumblob.Attach(ctx, q, albumblob.Photo{
				ID:          p.ID,
				WorkspaceID: deps.WorkspaceID,
				StorageKey:  p.StorageKey,
				ContentType: p.ContentType,
				ByteSize:    p.ByteSize,
				SHA256:      digest,
			}); err != nil {
				return err
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
			return nil, toAPIError(err)
		}
		// The thumbnail is attached after that commit, in a transaction of its
		// own, and nothing it can do reaches back. A photo whose bytes arrived
		// is never lost to a second, optional upload that did not: the picture
		// is already confirmed by the time this runs, and every way it can fail
		// leaves the photo exactly as it is, drawn from the picture itself.
		attachThumbnail(ctx, deps, p)
		// Re-read rather than patching the copy in hand: the confirm attached a
		// storage object, and the response presigns whatever the photo now
		// points at.
		confirmed, err := deps.Queries.GetAlbumPhotoByPublicID(ctx, p.PublicID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &ConfirmPhotoOutput{Body: mapPhoto(ctx, deps, cal, confirmed)}, nil
	}
}

// attachThumbnail moves a photo's grid-sized rendering onto the object model,
// if one was uploaded.
//
// Nothing here reports a failure to the caller, and that is the point rather
// than an oversight: the thumbnail is optional at every step. It may not be
// there at all -- the client never declared one, or declared one and did not
// send it -- and it may be there and refused. Each of those leaves a confirmed
// photo with no thumbnail, which is a state every read already falls back from.
// The alternative, failing the confirm, would answer "your picture is stored"
// with an error and lose the photo to a rendering nothing needs.
//
// The content type is taken from what landed rather than from the declaration,
// for the same reason the digest is: the presign is the only thing the client
// said, and this is the only thing that can say what is actually there.
func attachThumbnail(ctx context.Context, deps Deps, p generated.AlbumPhoto) {
	key := albumblob.ThumbnailKey(p.StorageKey)
	info, exists, err := deps.Storage.StatObject(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "failed to stat album thumbnail object", "photoID", p.ID, "key", key, "error", err)
		return
	}
	if !exists {
		return
	}
	if info.Size <= 0 || info.Size > maxThumbnailSize {
		slog.WarnContext(ctx, "album thumbnail object is not thumbnail-sized, leaving the photo without one",
			"photoID", p.ID, "size", info.Size)
		return
	}
	if !isImageContentType(info.ContentType) {
		slog.WarnContext(ctx, "album thumbnail object is not a renderable image, leaving the photo without one",
			"photoID", p.ID, "contentType", info.ContentType)
		return
	}
	digest, err := deps.Storage.SHA256(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "failed to digest album thumbnail object", "photoID", p.ID, "key", key, "error", err)
		return
	}
	if err := dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
		return albumblob.AttachThumbnail(ctx, q, albumblob.Photo{
			ID:          p.ID,
			WorkspaceID: deps.WorkspaceID,
			StorageKey:  key,
			ContentType: strings.ToLower(info.ContentType),
			ByteSize:    uint64(info.Size),
			SHA256:      digest,
		})
	}); err != nil {
		slog.WarnContext(ctx, "failed to attach album thumbnail object", "photoID", p.ID, "error", err)
	}
}

// deleteMismatchedPhoto removes an uploaded object (and its pending row) that
// failed the Confirm-time size check, so a mismatched upload does not linger
// as an orphan until the 7-day abandoned-upload sweep. Best-effort: errors are
// logged, not surfaced, since the caller is already returning the mismatch
// error to the client.
//
// Deleting the bytes is still safe here, unlike on the delete path: a
// reservation is not attached to a storage object until its digest has been
// read, so these bytes sit at a key belonging to this photo alone and no
// other row can be resolving to them.
func deleteMismatchedPhoto(ctx context.Context, deps Deps, p generated.AlbumPhoto) {
	if err := deps.Storage.DeleteObject(ctx, p.StorageKey); err != nil {
		slog.WarnContext(ctx, "failed to delete mismatched album photo object", "key", p.StorageKey, "error", err)
	}
	if err := deps.Queries.DeletePendingAlbumPhoto(ctx, generated.DeletePendingAlbumPhotoParams{
		ID:               p.ID,
		UploadedByUserID: p.UploadedByUserID,
	}); err != nil {
		slog.WarnContext(ctx, "failed to delete mismatched album photo row", "photoID", p.ID, "error", err)
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

// UpdatePhoto edits the caption and/or event link of a photo.
func UpdatePhoto(deps Deps) func(context.Context, *UpdatePhotoInput) (*UpdatePhotoOutput, error) {
	return func(ctx context.Context, in *UpdatePhotoInput) (*UpdatePhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			return nil, toAPIError(err)
		}

		caption := photo.Caption
		if in.Body.Caption != nil {
			caption = *in.Body.Caption
		}

		eventID := photo.CalendarEventID
		if in.Body.EventID != nil {
			if *in.Body.EventID == "" {
				eventID = sql.NullInt32{}
			} else {
				resolved, rerr := resolveEventForCalendar(ctx, deps, cal.ID, *in.Body.EventID)
				if rerr != nil {
					return nil, toAPIError(rerr)
				}
				eventID = resolved
			}
		}

		var refreshed generated.AlbumPhoto
		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.UpdateAlbumPhotoMeta(ctx, generated.UpdateAlbumPhotoMetaParams{
				Caption:         caption,
				CalendarEventID: eventID,
				ID:              photo.ID,
			}); err != nil {
				return err
			}
			var err error
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
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		return &UpdatePhotoOutput{Body: mapPhoto(ctx, deps, cal, refreshed)}, nil
	}
}

// DeletePhoto soft-deletes a photo and removes the underlying object best-effort.
func DeletePhoto(deps Deps) func(context.Context, *DeletePhotoInput) (*DeletePhotoOutput, error) {
	return func(ctx context.Context, in *DeletePhotoInput) (*DeletePhotoOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			return nil, toAPIError(err)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			return nil, toAPIError(err)
		}

		err = dbtx.Run(ctx, deps.DB, func(q *generated.Queries) error {
			if err := q.SoftDeleteAlbumPhoto(ctx, photo.ID); err != nil {
				return err
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
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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
			return nil, toAPIError(err)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			return nil, toAPIError(err)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		url, err := deps.Storage.PresignDownload(ctx, photoObjectKey(ctx, deps, photo), photoDownloadFilename(photo), downloadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		out := &DownloadPhotoOutput{}
		out.Body.DownloadURL = url
		return out, nil
	}
}
