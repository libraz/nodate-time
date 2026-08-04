-- name: GetOAuthProviderConfig :one
SELECT * FROM oauth_provider_configs WHERE provider = ?;

-- ListOAuthProviderConfigs never selects the ciphertext. The admin screen
-- shows which providers are configured, not their secrets, and a query
-- that returns one by habit is how a secret reaches a response body.
-- name: ListOAuthProviderConfigs :many
SELECT id, public_id, provider, client_id, enabled, updated_at, updated_by_user_id
FROM oauth_provider_configs
ORDER BY provider;

-- name: UpsertOAuthProviderConfig :exec
INSERT INTO oauth_provider_configs (public_id, provider, client_id, client_secret_ciphertext, enabled, updated_by_user_id)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  client_id = VALUES(client_id),
  client_secret_ciphertext = VALUES(client_secret_ciphertext),
  enabled = VALUES(enabled),
  updated_by_user_id = VALUES(updated_by_user_id);

-- name: SetOAuthProviderEnabled :exec
UPDATE oauth_provider_configs SET enabled = ?, updated_by_user_id = ? WHERE provider = ?;

-- name: DeleteOAuthProviderConfig :exec
DELETE FROM oauth_provider_configs WHERE provider = ?;
