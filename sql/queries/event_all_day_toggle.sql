-- ListEventReminderStarts reads the send records of an event so a change of
-- its all-day shape can rewrite the keys they hold.
-- name: ListEventReminderStarts :many
SELECT id, occurrence_start FROM calendar_event_reminders WHERE event_id = ?
ORDER BY occurrence_start;

-- name: SetEventReminderStart :exec
UPDATE calendar_event_reminders SET occurrence_start = ? WHERE id = ?;
