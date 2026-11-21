package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/handlers/albums"
)

func registerAlbums(g apiGroups, deps Deps) {
	api := g.protected
	albumDeps := albums.Deps{DB: deps.DB, Queries: deps.Queries, Storage: deps.Storage, WorkspaceID: deps.WorkspaceID}

	// Album
	huma.Register(api, huma.Operation{
		OperationID: "list-album-photos",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/albums",
		Summary:     "List photos in the calendar album",
		Tags:        []string{"Album"},
	}, albums.ListPhotos(albumDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "presign-album-photo",
		Method:        http.MethodPost,
		Path:          "/calendars/{calendarId}/albums/presign",
		Summary:       "Get a presigned URL for uploading an album photo",
		Tags:          []string{"Album"},
		DefaultStatus: http.StatusCreated,
	}, albums.PresignUpload(albumDeps))

	huma.Register(api, huma.Operation{
		OperationID: "confirm-album-photo",
		Method:      http.MethodPost,
		Path:        "/calendars/{calendarId}/albums/{photoId}/confirm",
		Summary:     "Confirm a previously uploaded album photo",
		Tags:        []string{"Album"},
		Errors:      errorStatuses(apierrors.UploadDigestMismatch, apierrors.AlbumPhotoNotFound),
	}, albums.ConfirmPhoto(albumDeps))

	huma.Register(api, huma.Operation{
		OperationID: "update-album-photo",
		Method:      http.MethodPatch,
		Path:        "/calendars/{calendarId}/albums/{photoId}",
		Summary:     "Update an album photo's caption or linked event",
		Tags:        []string{"Album"},
		Errors:      preconditionedErrors(apierrors.AlbumStale, apierrors.AlbumPhotoNotFound, apierrors.EventNotFound, apierrors.Validation),
	}, albums.UpdatePhoto(albumDeps))

	huma.Register(api, huma.Operation{
		OperationID:   "delete-album-photo",
		Method:        http.MethodDelete,
		Path:          "/calendars/{calendarId}/albums/{photoId}",
		Summary:       "Delete an album photo",
		Tags:          []string{"Album"},
		DefaultStatus: 204,
		Errors:        preconditionedErrors(apierrors.AlbumStale, apierrors.AlbumPhotoNotFound),
	}, albums.DeletePhoto(albumDeps))

	huma.Register(api, huma.Operation{
		OperationID: "get-album-photo-download",
		Method:      http.MethodGet,
		Path:        "/calendars/{calendarId}/albums/{photoId}/download",
		Summary:     "Get a presigned download URL for a single photo",
		Tags:        []string{"Album"},
	}, albums.GetDownload(albumDeps))
}
