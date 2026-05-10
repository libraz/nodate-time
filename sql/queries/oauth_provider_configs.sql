-- name: GetOAuthProviderConfig :one
SELECT provider, client_id, client_secret_enc, enabled, updated_at, updated_by
FROM oauth_provider_configs
WHERE provider = ? LIMIT 1;

-- name: ListOAuthProviderConfigs :many
SELECT provider, client_id, enabled, updated_at, updated_by
FROM oauth_provider_configs
ORDER BY provider;

-- name: UpsertOAuthProviderConfig :exec
INSERT INTO oauth_provider_configs (provider, client_id, client_secret_enc, enabled, updated_by)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  client_id = VALUES(client_id),
  client_secret_enc = VALUES(client_secret_enc),
  enabled = VALUES(enabled),
  updated_by = VALUES(updated_by);

-- name: SetOAuthProviderEnabled :exec
UPDATE oauth_provider_configs SET enabled = ?, updated_by = ? WHERE provider = ?;

-- name: DeleteOAuthProviderConfig :exec
DELETE FROM oauth_provider_configs WHERE provider = ?;
