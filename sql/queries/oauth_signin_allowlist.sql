-- name: ListOauthSigninAllowlistEmails :many
-- The admin screen lists the live email entries only; domain entries are
-- managed elsewhere and withdrawn rows are not shown.
SELECT
  public_id,
  entry_value,
  notes,
  created_at
FROM oauth_signin_allowlist
WHERE entry_kind = 'email' AND enabled = TRUE
ORDER BY entry_value;

-- name: IsSigninAdmittedByAllowlist :one
-- Whether a live entry admits the address: an email entry equal to it, or a
-- domain entry equal to the part after its final '@'. Both arguments are
-- already lower case; entry_value is compared byte-exact.
SELECT EXISTS (
  SELECT 1 FROM oauth_signin_allowlist
  WHERE enabled = TRUE
    AND ((entry_kind = 'email' AND entry_value = sqlc.arg(email))
      OR (entry_kind = 'domain' AND entry_value = sqlc.arg(domain)))
) AS admitted;

-- name: CountEnabledOauthSigninAllowlistEntries :one
-- One live row of either kind turns the sign-in restriction on.
SELECT COUNT(*) AS entries FROM oauth_signin_allowlist WHERE enabled = TRUE;

-- name: GetOauthSigninAllowlistEmail :one
-- The row holding the address, withdrawn or not: a withdrawn row keeps its
-- (entry_kind, entry_value) claim.
SELECT public_id, entry_value, notes, enabled, created_at
FROM oauth_signin_allowlist
WHERE entry_kind = 'email' AND entry_value = ?;

-- name: UpsertOauthSigninAllowlistEmail :exec
-- Adds an email entry, or revives the withdrawn row that holds the address.
INSERT INTO oauth_signin_allowlist (public_id, added_by_user_id, entry_kind, entry_value, notes)
VALUES (?, ?, 'email', ?, ?)
ON DUPLICATE KEY UPDATE
  public_id = VALUES(public_id),
  added_by_user_id = VALUES(added_by_user_id),
  notes = VALUES(notes),
  enabled = TRUE;

-- name: WithdrawOauthSigninAllowlistEmail :execresult
-- Clears enabled rather than deleting, so the address can be allowed again.
-- Takes the public id, the only identifier the API exposes.
UPDATE oauth_signin_allowlist SET enabled = FALSE
WHERE public_id = ? AND entry_kind = 'email' AND enabled = TRUE;
