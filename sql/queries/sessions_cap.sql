-- Live sessions the 'time' audience keeps per user. Opening one more revokes
-- the oldest rather than refusing the sign-in; other audiences are never
-- touched.

-- ListSessionsOverCap names the live sessions beyond the newest `keep`. It is a
-- plain read, and the revocation goes by primary key: a locking range scan of
-- the user's sessions would gap-lock the index that every concurrent sign-in
-- inserts into, and two sign-ins of different users deadlock on it.
-- name: ListSessionsOverCap :many
SELECT s.id FROM sessions s
WHERE s.user_id = sqlc.arg(user_id)
  AND s.audience = 'time'
  AND s.revoked_at IS NULL
  AND s.expires_at > NOW(3)
ORDER BY s.created_at DESC, s.id DESC
LIMIT 1000 OFFSET ?;
