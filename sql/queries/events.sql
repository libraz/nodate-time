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
--
-- The actor's avatar object is joined in with the row. A feed page is up to
-- two hundred entries and looking the key up per entry would be two hundred
-- reads for a handful of distinct people.
-- name: ListEventsByCalendar :many
SELECT e.id, e.public_id, e.type, e.payload_json, e.occurred_at,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url,
       so.storage_key AS actor_avatar_storage_key
FROM events e
LEFT JOIN users u ON u.id = e.actor_user_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
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
-- points at it. There is deliberately no second table keyed by entity: two
-- records of who changed what eventually disagree, and then neither can be
-- trusted.
--
-- Matched on subject_public_id, the stored generated column that lifts the
-- subject out of payload_json. Reading the JSON directly is not indexable,
-- so the same question used to scan every row the calendar had ever
-- produced -- and LIMIT applies after the scan, not to it.
-- name: ListEventsBySubject :many
SELECT e.id, e.public_id, e.type, e.payload_json, e.occurred_at,
       u.public_id AS actor_public_id, u.display_name AS actor_display_name,
       u.avatar_url AS actor_avatar_url,
       so.storage_key AS actor_avatar_storage_key
FROM events e
LEFT JOIN users u ON u.id = e.actor_user_id
LEFT JOIN storage_objects so ON so.id = u.avatar_storage_object_id
WHERE e.workspace_id = ?
  AND e.calendar_id = ?
  AND e.subject_public_id = sqlc.arg(subject_id)
ORDER BY e.id DESC
LIMIT ?;

-- name: ListEventsSince :many
SELECT * FROM events
WHERE id > sqlc.arg(after_id)
ORDER BY id
LIMIT ?;
