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
	uploadTTL    = 15 * time.Minute
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
	id                       uint32
	publicID                 []byte
	caption                  string
	contentType              string
	byteSize                 uint64
	width                    sql.NullInt32
	height                   sql.NullInt32
	storageKey               string
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
			storageKey:               r.StorageKey,
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
			storageKey:               r.StorageKey,
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
		if url, err := deps.Storage.PresignGet(ctx, p.StorageKey, imageTTL); err == nil {
			resp.ImageURL = url
		} else {
			slog.WarnContext(ctx, "failed to presign album photo URL", "photoID", p.ID, "error", err)
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

// PresignUpload issues a presigned PUT URL for adding a photo to the album.
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
		p.Enabled = true
		return &ConfirmPhotoOutput{Body: mapPhoto(ctx, deps, cal, p)}, nil
	}
}

// deleteMismatchedPhoto removes an uploaded object (and its pending row) that
// failed the Confirm-time size check, so a mismatched upload does not linger
// as an orphan until the 7-day abandoned-upload sweep. Best-effort: errors are
// logged, not surfaced, since the caller is already returning the mismatch
// error to the client.
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
		if deps.Storage != nil {
			if derr := deps.Storage.DeleteObject(ctx, photo.StorageKey); derr != nil {
				slog.WarnContext(ctx, "failed to delete album photo object", "key", photo.StorageKey, "error", derr)
			}
		}
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
		url, err := deps.Storage.PresignDownload(ctx, photo.StorageKey, photoDownloadFilename(photo), downloadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		out := &DownloadPhotoOutput{}
		out.Body.DownloadURL = url
		return out, nil
	}
}
