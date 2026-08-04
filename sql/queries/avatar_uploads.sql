-- name: CreateAvatarUpload :execresult
INSERT INTO avatar_uploads (
  public_id, user_id, sha256, storage_key, content_type, byte_size, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: CountActiveAvatarUploads :one
SELECT COUNT(*) FROM avatar_uploads
WHERE user_id = ? AND expires_at > CURRENT_TIMESTAMP(3);

-- name: GetAvatarUploadForUser :one
SELECT * FROM avatar_uploads
WHERE public_id = ? AND user_id = ? AND expires_at > CURRENT_TIMESTAMP(3)
LIMIT 1;

-- name: GetAvatarUploadForUserForUpdate :one
SELECT * FROM avatar_uploads
WHERE id = ? AND user_id = ? AND expires_at > CURRENT_TIMESTAMP(3)
LIMIT 1
FOR UPDATE;

-- name: DeleteAvatarUpload :exec
DELETE FROM avatar_uploads WHERE id = ?;

-- name: ListExpiredAvatarUploads :many
SELECT * FROM avatar_uploads WHERE expires_at <= ? ORDER BY id LIMIT 500;

-- name: DeleteExpiredAvatarUpload :execresult
DELETE FROM avatar_uploads WHERE id = ? AND expires_at <= ?;
