-- The append-only event log.
--
-- Obligation 2 of the shared contract: every state change appends exactly
-- one row here, in the same transaction as the rows it describes. This is
-- also what the activity feed reads -- there is no second history table,
-- because two records of who changed what eventually disagree.
--
-- Nothing updates or deletes a row. A correction is a new row.

-- name: AppendEvent :execresult
INSERT INTO events (public_id, workspace_id, calendar_id, actor_user_id, type, payload_json, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- ListEventsByCalendar is the calendar activity feed. Keyset-paginated by
-- id alone: id is strictly monotonic in insertion order, so it already
-- orders by time, and using one column keeps the ORDER BY and the cursor
-- from ever disagreeing about ties.
-- name: ListEventsByCalendar :many
SELECT e.id, e.public_id, e.type, e.payload_json, e.occurred_at,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url
FROM events e
LEFT JOIN users u ON u.id = e.actor_user_id
WHERE e.workspace_id = ?
  AND e.calendar_id = ?
  AND (sqlc.arg(after_id) = 0 OR e.id < sqlc.arg(after_id))
ORDER BY e.id DESC
LIMIT ?;

-- ListEventsByType filters the same feed to one entity's history. The type
-- prefix is matched by the caller rather than parsed here, so a new event
-- kind needs no query change.
-- name: ListEventsByType :many
SELECT e.id, e.public_id, e.type, e.payload_json, e.occurred_at,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url
FROM events e
LEFT JOIN users u ON u.id = e.actor_user_id
WHERE e.workspace_id = ?
  AND e.calendar_id = ?
  AND e.type LIKE sqlc.arg(type_prefix)
ORDER BY e.id DESC
LIMIT ?;

-- name: ListEventsSince :many
SELECT * FROM events
WHERE id > sqlc.arg(after_id)
ORDER BY id
LIMIT ?;
