-- ListCalendarLabels is a calendar's labels in creation order. It has no
-- LIMIT: the set is read whole, and CountCalendarLabels bounds it at write time.
-- name: ListCalendarLabels :many
SELECT * FROM calendar_labels
WHERE calendar_id = ?
ORDER BY id;

-- CountCalendarLabels is read under the calendar's manage lock by the write
-- that would add to it.
-- name: CountCalendarLabels :one
SELECT COUNT(*) FROM calendar_labels WHERE calendar_id = ?;

-- name: GetCalendarLabelByPublicID :one
SELECT * FROM calendar_labels WHERE public_id = ? AND calendar_id = ?;

-- GetCalendarLabelForUpdate is the read a write merges over, under the row lock.
-- name: GetCalendarLabelForUpdate :one
SELECT * FROM calendar_labels WHERE public_id = ? AND calendar_id = ? FOR UPDATE;

-- name: CreateCalendarLabel :exec
INSERT INTO calendar_labels (public_id, workspace_id, calendar_id, name, color)
VALUES (?, ?, ?, ?, ?);

-- name: UpdateCalendarLabel :exec
UPDATE calendar_labels SET name = ?, color = ? WHERE id = ?;

-- DeleteCalendarLabel removes a label; the links of events filed under it go
-- with it through the foreign key.
-- name: DeleteCalendarLabel :exec
DELETE FROM calendar_labels WHERE id = ?;

-- name: GetEventLabelID :one
SELECT label_id FROM calendar_event_labels WHERE event_id = ?;

-- name: SetEventLabel :exec
INSERT INTO calendar_event_labels (event_id, label_id)
VALUES (?, ?)
ON DUPLICATE KEY UPDATE label_id = VALUES(label_id);

-- name: DeleteEventLabel :exec
DELETE FROM calendar_event_labels WHERE event_id = ?;

-- ListEventLabelsByEvents reads the label of each event row a response draws.
-- A label of another calendar never matches, so an event moved between
-- calendars outside this product cannot show a foreign label's colour.
-- name: ListEventLabelsByEvents :many
SELECT el.event_id, l.public_id AS label_public_id, l.color
FROM calendar_event_labels el
JOIN calendar_events e ON e.id = el.event_id
JOIN calendar_labels l ON l.id = el.label_id AND l.calendar_id = e.calendar_id
WHERE el.event_id IN (sqlc.slice('event_ids'));
