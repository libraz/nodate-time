package events

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/libraz/nodate-time/apps/api/internal/db/generated"
	apierrors "github.com/libraz/nodate-time/apps/api/internal/errors"
	"github.com/libraz/nodate-time/apps/api/internal/http/middleware"
)

const maxAttachmentSize = 100 * 1024 * 1024 // 100 MB

func mapAttachment(att generated.EventAttachment) AttachmentResponse {
	return AttachmentResponse{
		ID:          pubIDToHex(att.PublicID),
		Filename:    att.Filename,
		ContentType: att.ContentType,
		ByteSize:    att.ByteSize,
		CreatedAt:   att.CreatedAt,
	}
}

// PresignUpload generates a presigned URL for uploading a file attachment.
func PresignUpload(deps Deps) func(context.Context, *PresignUploadInput) (*PresignUploadOutput, error) {
	return func(ctx context.Context, in *PresignUploadInput) (*PresignUploadOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		if in.Body.ByteSize > maxAttachmentSize {
			return nil, apierrors.ToHuma(apierrors.AttachmentTooLarge)
		}

		contentType := in.Body.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		attachPubID, _ := uuid.NewV7()
		calPubHex := pubIDToHex(cal.PublicID)
		evtPubHex := pubIDToHex(evt.PublicID)
		attachPubHex := attachPubID.String()
		storageKey := fmt.Sprintf("attachments/%s/%s/%s/%s", calPubHex, evtPubHex, attachPubHex, in.Body.Filename)

		_, err = deps.Queries.CreateEventAttachment(ctx, generated.CreateEventAttachmentParams{
			PublicID:    attachPubID[:],
			EventID:     evt.ID,
			UploadedBy:  userID,
			Filename:    in.Body.Filename,
			ContentType: contentType,
			ByteSize:    in.Body.ByteSize,
			StorageKey:  storageKey,
		})
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		url, err := deps.Storage.PresignPut(ctx, storageKey, contentType, 15*time.Minute)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &PresignUploadOutput{}
		out.Body.AttachmentID = attachPubHex
		out.Body.UploadURL = url
		return out, nil
	}
}

// ListAttachments returns all active attachments for an event.
func ListAttachments(deps Deps) func(context.Context, *ListAttachmentsInput) (*ListAttachmentsOutput, error) {
	return func(ctx context.Context, in *ListAttachmentsInput) (*ListAttachmentsOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		rows, err := deps.Queries.ListEventAttachments(ctx, evt.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		out := &ListAttachmentsOutput{Body: make([]AttachmentResponse, 0, len(rows))}
		for _, att := range rows {
			out.Body = append(out.Body, mapAttachment(att))
		}
		return out, nil
	}
}

// GetAttachmentDownload generates a presigned download URL for an attachment.
func GetAttachmentDownload(deps Deps) func(context.Context, *GetAttachmentDownloadInput) (*GetAttachmentDownloadOutput, error) {
	return func(ctx context.Context, in *GetAttachmentDownloadInput) (*GetAttachmentDownloadOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		_, err := resolveCalendar(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		attPub, err := parseUUID(in.AttachmentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		att, err := deps.Queries.GetAttachmentByPublicID(ctx, attPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}

		if deps.Storage == nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		url, err := deps.Storage.PresignGet(ctx, att.StorageKey, 5*time.Minute)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.StorageUnavailable)
		}

		out := &GetAttachmentDownloadOutput{}
		out.Body.DownloadURL = url
		return out, nil
	}
}

// DeleteAttachment soft-deletes an attachment from an event.
func DeleteAttachment(deps Deps) func(context.Context, *DeleteAttachmentInput) (*DeleteAttachmentOutput, error) {
	return func(ctx context.Context, in *DeleteAttachmentInput) (*DeleteAttachmentOutput, error) {
		userID, _ := middleware.ActorFromContext(ctx)
		cal, err := resolveCalendarWrite(ctx, deps, in.CalendarID, userID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		evt, err := resolveEvent(ctx, deps, cal.ID, in.EventID)
		if err != nil {
			if spec, ok := err.(*apierrors.Spec); ok {
				return nil, apierrors.ToHuma(spec)
			}
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		attPub, err := parseUUID(in.AttachmentID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		att, err := deps.Queries.GetAttachmentByPublicID(ctx, attPub)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}
		if att.EventID != evt.ID {
			return nil, apierrors.ToHuma(apierrors.AttachmentNotFound)
		}

		err = deps.Queries.SoftDeleteAttachment(ctx, att.ID)
		if err != nil {
			return nil, apierrors.ToHuma(apierrors.InternalUnexpected)
		}

		return &DeleteAttachmentOutput{}, nil
	}
}
