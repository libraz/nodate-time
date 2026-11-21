-- SetSeriesRule rewrites a series' rule and the end boundary derived from it,
-- leaving every other column alone. A split cuts the original series short
-- with this.
-- name: SetSeriesRule :exec
UPDATE calendar_events
SET recurrence_rule = ?, recurrence_end = ?
WHERE id = ?;

-- MoveRecurrenceOverride hands an override row to another series, keyed by the
-- occurrence it names on that series' grid. The dates move with it, computed
-- by the caller for the same reason RetimeRecurrenceOverride's are.
-- name: MoveRecurrenceOverride :exec
UPDATE calendar_events
SET recurrence_parent_id = ?, recurrence_original_start = ?, start_at = ?, end_at = ?
WHERE id = ?;

-- MoveEventReminder hands a send record to another series under the instant
-- its occurrence has there, so a reminder already sent is not sent again.
-- name: MoveEventReminder :exec
UPDATE calendar_event_reminders
SET event_id = ?, occurrence_start = ?
WHERE id = ?;

-- ListLiveSplitSeries returns the live series a following split cut from the
-- series whose public id is split_from, read off their creation entries.
-- name: ListLiveSplitSeries :many
SELECT ce.id FROM calendar_events ce
JOIN events ev ON ev.subject_public_id = ce.public_id
WHERE ev.workspace_id = sqlc.arg(workspace_id) AND ev.calendar_id = sqlc.arg(calendar_id)
  AND ev.type = 'calendar.event.created'
  AND JSON_UNQUOTE(JSON_EXTRACT(ev.payload_json, '$.splitFrom')) = sqlc.arg(split_from)
  AND ce.enabled = TRUE;
