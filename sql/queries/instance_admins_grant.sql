-- Granting instance administrator rights and the bounded allow-list. Both
-- lists are capped at write time, so the count that decides a cap is read
-- FOR UPDATE in the transaction that adds the row.

-- CountAllowlistEmailsForUpdate counts the live email entries; domain entries
-- are not part of the email cap.
-- name: CountAllowlistEmailsForUpdate :one
SELECT COUNT(*) FROM oauth_signin_allowlist
WHERE entry_kind = 'email' AND enabled = TRUE FOR UPDATE;
