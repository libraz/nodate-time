-- name: GetScheduleAvailabilitySharing :one
SELECT EXISTS (
  SELECT 1 FROM schedule_availability_calendars WHERE calendar_id = ? AND enabled = TRUE
) AS enabled;

-- name: SetScheduleAvailabilitySharing :exec
INSERT INTO schedule_availability_calendars (calendar_id, enabled)
VALUES (?, ?) ON DUPLICATE KEY UPDATE enabled = VALUES(enabled);

-- A shared calendar has at least two active members. The default scope
-- contains just the destination calendar, even for a workspace admin.
-- name: ListScheduleAvailabilityCalendars :many
SELECT c.id FROM calendars c
WHERE c.workspace_id = ? AND c.enabled = TRUE AND c.kind = 'personal'
  AND (c.id = sqlc.arg(calendar_id) OR (
    EXISTS (SELECT 1 FROM schedule_availability_calendars s WHERE s.calendar_id = c.id AND s.enabled = TRUE)
    AND (SELECT COUNT(*) FROM calendar_members cm
      JOIN workspace_members wm ON wm.workspace_id = c.workspace_id AND wm.user_id = cm.user_id AND wm.enabled = TRUE
      JOIN users u ON u.id = cm.user_id AND u.enabled = TRUE
      WHERE cm.calendar_id = c.id AND cm.workspace_id = c.workspace_id AND cm.enabled = TRUE) >= 2
  ))
ORDER BY c.id
LIMIT ?;

-- name: ListScheduleAvailabilityMembers :many
SELECT cm.user_id FROM calendar_members cm
JOIN workspace_members wm ON wm.workspace_id = cm.workspace_id AND wm.user_id = cm.user_id AND wm.enabled = TRUE
JOIN users u ON u.id = cm.user_id AND u.enabled = TRUE
WHERE cm.workspace_id = ? AND cm.calendar_id = ? AND cm.enabled = TRUE
  AND cm.user_id IN (sqlc.slice('member_ids'));

-- Matching an override must be independent of matching its series. An
-- occurrence can gain or lose a participant, or move across the window.
-- SERIALIZABLE holds the ranges this statement scans. A workspace index
-- can look cheap for a sparse calendar but would lock unrelated events.
-- name: ListScheduleAvailabilityEvents :many
SELECT e.id, e.all_day, e.owner_user_id, e.show_as, e.flexibility, e.start_at, e.end_at, e.timezone,
       e.recurrence_rule, e.recurrence_exceptions, e.recurrence_parent_id, e.recurrence_original_start
FROM calendar_events e FORCE INDEX (idx_calendar_events_calendar_range)
WHERE e.workspace_id = ? AND e.calendar_id = ? AND e.enabled = TRUE
  AND e.start_at < sqlc.arg(range_end)
  AND ((e.recurrence_rule IS NULL AND e.end_at > sqlc.arg(range_start) AND e.end_at > e.start_at)
    OR (e.recurrence_rule IS NOT NULL AND (e.recurrence_end IS NULL OR e.recurrence_end >= sqlc.arg(recurrence_floor)
        OR e.recurrence_end = '2099-12-31 23:59:59')))
  AND (e.owner_user_id IN (sqlc.slice('owner_ids'))
    OR EXISTS (SELECT 1 FROM calendar_event_attendees a FORCE INDEX (idx_calendar_event_attendees_event_user)
      WHERE a.workspace_id = e.workspace_id AND a.event_id = e.id AND a.enabled = TRUE
        AND a.user_id IN (sqlc.slice('attendee_ids')) AND a.rsvp <> 'declined'))
LIMIT ?;

-- name: ListScheduleAvailabilityParents :many
SELECT e.id, e.all_day, e.owner_user_id, e.show_as, e.flexibility, e.start_at, e.end_at, e.timezone,
       e.recurrence_rule, e.recurrence_exceptions, e.recurrence_parent_id, e.recurrence_original_start
FROM calendar_events e FORCE INDEX (PRIMARY)
WHERE e.workspace_id = ? AND e.calendar_id = ? AND e.enabled = TRUE
  AND e.id IN (sqlc.slice('parent_ids')) AND e.recurrence_rule IS NOT NULL AND e.recurrence_parent_id IS NULL;

-- Read even overrides whose own dates or participants are outside the
-- search. They suppress the original occurrence on its original date.
-- name: ListScheduleAvailabilityOverrides :many
SELECT e.id, e.all_day, e.owner_user_id, e.show_as, e.flexibility, e.start_at, e.end_at, e.timezone,
       e.recurrence_rule, e.recurrence_exceptions, e.recurrence_parent_id, e.recurrence_original_start
FROM calendar_events e FORCE INDEX (uniq_calendar_events_recurrence_override)
WHERE e.workspace_id = ? AND e.calendar_id = ? AND e.enabled = TRUE
  AND e.recurrence_parent_id IN (sqlc.slice('parent_ids'))
ORDER BY e.recurrence_parent_id, e.recurrence_original_start
LIMIT ?;
