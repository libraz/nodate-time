package albums

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
	"github.com/libraz/nodate-time/apps/api/internal/storage"
)

const (
	maxPhotoSize    = 20 * 1024 * 1024
	uploadTTL       = 15 * time.Minute
	downloadTTL     = 5 * time.Minute
	defaultPageSize = 30
	maxPageSize     = 100
)

type Deps struct {
	DB      *sql.DB
	Queries *generated.Queries
	Storage *storage.Client
}

func pubIDToHex(b []byte) string {
	u, err := uuid.FromBytes(b)
	if err != nil {
		return hex.EncodeToString(b)
	}
	return u.String()
}

func parseUUID(s string) ([]byte, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, err
	}
	return u[:], nil
}

func resolveCalendar(ctx context.Context, deps Deps, calPubID string, userID uint32) (generated.Calendar, error) {
	pubBytes, err := parseUUID(calPubID)
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarNotFound
	}
	cal, err := deps.Queries.GetCalendarByPublicID(ctx, pubBytes)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return generated.Calendar{}, apierrors.CalendarNotFound
		}
		return generated.Calendar{}, apierrors.InternalUnexpected
	}
	_, err = deps.Queries.GetCalendarMember(ctx, generated.GetCalendarMemberParams{
		CalendarID: cal.ID,
		UserID:     userID,
	})
	if err != nil {
		return generated.Calendar{}, apierrors.CalendarAccessDenied
	}
	return cal, nil
}

func isImageContentType(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	return strings.HasPrefix(ct, "image/")
}

func photoStorageKey(calPubHex, photoPubHex string) string {
	return fmt.Sprintf("albums/%s/%s", calPubHex, photoPubHex)
}

// encodeCursor turns a (takenAt, id) tuple into an opaque base64 cursor.
func encodeCursor(takenAt time.Time, id uint32) string {
	raw := fmt.Sprintf("%d:%d", takenAt.UnixMilli(), id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, uint32, error) {
	if s == "" {
		return time.Time{}, 0, fmt.Errorf("empty cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, 0, err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("malformed cursor")
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	idVal, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return time.Time{}, 0, err
	}
	return time.UnixMilli(ms).UTC(), uint32(idVal), nil
}

func eventPubIDForResponse(ctx context.Context, deps Deps, evtID sql.NullInt32) string {
	if !evtID.Valid {
		return ""
	}
	evt, err := deps.Queries.GetEventByID(ctx, uint32(evtID.Int32))
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
	resp := AlbumUploader{ID: pubIDToHex(u.PublicID), Name: u.Name}
	if deps.Storage != nil && u.AvatarStorageKey.Valid && u.AvatarStorageKey.String != "" {
		if url, err := deps.Storage.PresignGet(ctx, u.AvatarStorageKey.String, downloadTTL); err == nil {
			resp.AvatarURL = url
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
		ByteSize:    p.ByteSize,
		TakenAt:     p.TakenAt,
		CreatedAt:   p.CreatedAt,
		EventID:     eventPubIDForResponse(ctx, deps, p.EventID),
		UploadedBy:  uploaderForResponse(ctx, deps, p.UploadedBy),
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
		if url, err := deps.Storage.PresignGet(ctx, p.StorageKey, downloadTTL); err == nil {
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
	evt, err := deps.Queries.GetEventByPublicID(ctx, pub)
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
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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

		var photos []generated.AlbumPhoto
		if in.Cursor == "" {
			photos, err = deps.Queries.ListAlbumPhotosFirstPage(ctx, generated.ListAlbumPhotosFirstPageParams{
				CalendarID: cal.ID,
				Limit:      fetchLimit,
			})
		} else {
			takenAt, idBefore, derr := decodeCursor(in.Cursor)
			if derr != nil {
				return nil, apierrors.ToHuma(apierrors.BadRequest)
			}
			photos, err = deps.Queries.ListAlbumPhotosAfter(ctx, generated.ListAlbumPhotosAfterParams{
				CalendarID:  cal.ID,
				TakenBefore: takenAt,
				IDBefore:    idBefore,
				Limit:       fetchLimit,
			})
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
		for _, p := range photos {
			out.Body.Items = append(out.Body.Items, mapPhoto(ctx, deps, cal, p))
		}
		if hasMore && len(photos) > 0 {
			last := photos[len(photos)-1]
			out.Body.NextCursor = encodeCursor(last.TakenAt, last.ID)
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}
		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		if !isImageContentType(in.Body.ContentType) {
			return nil, apierrors.ToHuma(apierrors.InvalidImageContentType)
		}
		if in.Body.ByteSize > maxPhotoSize {
			return nil, apierrors.ToHuma(apierrors.AlbumPhotoTooLarge)
		}

		eventID, err := resolveEventForCalendar(ctx, deps, cal.ID, in.Body.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
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
			PublicID:    photoPubID[:],
			CalendarID:  cal.ID,
			UploadedBy:  userID,
			EventID:     eventID,
			Caption:     in.Body.Caption,
			ContentType: strings.ToLower(in.Body.ContentType),
			ByteSize:    in.Body.ByteSize,
			Width:       width,
			Height:      height,
			StorageKey:  key,
			TakenAt:     takenAt,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		url, err := deps.Storage.PresignPut(ctx, key, in.Body.ContentType, uploadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &PresignPhotoOutput{}
		out.Body.PhotoID = photoPubHex
		out.Body.UploadURL = url
		out.Body.StorageKey = key
		return out, nil
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		caption := photo.Caption
		if in.Body.Caption != nil {
			caption = *in.Body.Caption
		}

		eventID := photo.EventID
		if in.Body.EventID != nil {
			if *in.Body.EventID == "" {
				eventID = sql.NullInt32{}
			} else {
				resolved, rerr := resolveEventForCalendar(ctx, deps, cal.ID, *in.Body.EventID)
				if rerr != nil {
					if spec, ok := rerr.(*apierrors.Spec); ok {
						return nil, apierrors.ToHuma(spec)
					}
					return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
				}
				eventID = resolved
			}
		}

		err = deps.Queries.UpdateAlbumPhotoMeta(ctx, generated.UpdateAlbumPhotoMetaParams{
			Caption: caption,
			EventID: eventID,
			ID:      photo.ID,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		refreshed, err := deps.Queries.GetAlbumPhotoByPublicID(ctx, photo.PublicID)
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
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if err := deps.Queries.SoftDeleteAlbumPhoto(ctx, photo.ID); err != nil {
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
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		photo, err := loadPhotoForCalendar(ctx, deps, cal.ID, in.PhotoID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		url, err := deps.Storage.PresignGet(ctx, photo.StorageKey, downloadTTL)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}
		out := &DownloadPhotoOutput{}
		out.Body.DownloadURL = url
		return out, nil
	}
}
