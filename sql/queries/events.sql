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
--
-- The cursor a client receives names the row by its public id; see
-- GetEventIDByPublicID. The internal id is a single deployment-wide sequence,
-- so handing it out tells anyone holding two cursors how much the whole
-- instance wrote in between.
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

-- GetEventIDByPublicID resolves the cursor a client was handed back to the
-- row it names. The feed pages by the internal id because that is what orders
-- strictly by insertion, but nothing outside ever sees that number.
--
-- Scoped to the calendar the cursor is being used on: a cursor from a
-- different feed names a real row, and resolving it would silently start this
-- feed from an unrelated position rather than saying the cursor is wrong.
-- name: GetEventIDByPublicID :one
SELECT id FROM events
WHERE workspace_id = ? AND calendar_id = ? AND public_id = ?;

-- ListEventsBySubject is one entity's history: every log row whose payload
-- names it. The subject is matched inside payload_json rather than through
-- a column, because the log records public ids and the contract gives the
-- payload no fixed schema beyond that.
--
-- The (workspace_id, calendar_id, occurred_at) index bounds the scan to one
-- calendar's history, which is the same set the activity feed already pages
-- through. There is deliberately no second table keyed by entity: two
-- records of who changed what eventually disagree, and then neither can be
-- trusted.
-- name: ListEventsBySubject :many
SELECT e.id, e.public_id, e.type, e.payload_json, e.occurred_at,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url
FROM events e
LEFT JOIN users u ON u.id = e.actor_user_id
WHERE e.workspace_id = ?
  AND e.calendar_id = ?
  AND e.payload_json->>'$.id' = CAST(sqlc.arg(subject_id) AS CHAR)
ORDER BY e.id DESC
LIMIT ?;

-- name: ListEventsSince :many
SELECT * FROM events
WHERE id > sqlc.arg(after_id)
ORDER BY id
LIMIT ?;
