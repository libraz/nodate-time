-- Attendee reads name current members only. The participant list is also the
-- write format, and a write accepts members only, so a list naming somebody
-- who has left the calendar could never be saved back.
-- name: ListEventAttendees :many
SELECT a.user_id, a.rsvp, a.can_edit, u.public_id AS user_public_id,
       u.display_name, u.avatar_url
FROM calendar_event_attendees a
INNER JOIN users u ON u.id = a.user_id
INNER JOIN calendar_events e ON e.id = a.event_id
INNER JOIN calendar_members cm
  ON cm.calendar_id = e.calendar_id AND cm.user_id = a.user_id AND cm.enabled = TRUE
WHERE a.event_id = ? AND a.enabled = TRUE
ORDER BY a.created_at;

-- ListEventAttendeesByEvents answers for a whole listing in one round trip.
-- Every event in a month carries a participant list, and asking per event made
-- rendering a calendar cost one query per event on it.
-- name: ListEventAttendeesByEvents :many
SELECT a.event_id, a.user_id, a.rsvp, a.can_edit, u.public_id AS user_public_id,
       u.display_name, u.avatar_url
FROM calendar_event_attendees a
INNER JOIN users u ON u.id = a.user_id
INNER JOIN calendar_events e ON e.id = a.event_id
INNER JOIN calendar_members cm
  ON cm.calendar_id = e.calendar_id AND cm.user_id = a.user_id AND cm.enabled = TRUE
WHERE a.event_id IN (sqlc.slice('event_ids')) AND a.enabled = TRUE
ORDER BY a.event_id, a.created_at;

-- AddEventAttendee revives a removed row rather than inserting beside it,
-- so re-adding somebody does not leave two rows for one person. A revived row
-- comes back without its edit grant: only the owner's side may give that, and
-- whoever re-adds a participant need not be.
-- name: AddEventAttendee :exec
INSERT INTO calendar_event_attendees (public_id, workspace_id, event_id, user_id)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE can_edit = IF(enabled, can_edit, FALSE), enabled = TRUE;

-- ListEnabledEventAttendeeIDs is every live attendee row of an event, members
-- or not, so a participant replacement can disable whoever is not kept.
-- name: ListEnabledEventAttendeeIDs :many
SELECT user_id FROM calendar_event_attendees
WHERE event_id = ? AND enabled = TRUE;

-- name: DisableEventAttendee :exec
UPDATE calendar_event_attendees SET enabled = FALSE, can_edit = FALSE
WHERE event_id = ? AND user_id = ?;

-- DisableCalendarAttendee takes a departing member off every event of the
-- calendar they leave, overrides included, along with any edit grant.
-- name: DisableCalendarAttendee :exec
UPDATE calendar_event_attendees SET enabled = FALSE, can_edit = FALSE
WHERE user_id = sqlc.arg(user_id) AND enabled = TRUE
  AND event_id IN (SELECT id FROM calendar_events WHERE calendar_id = sqlc.arg(calendar_id));

-- name: GetEventAttendee :one
SELECT * FROM calendar_event_attendees
WHERE event_id = ? AND user_id = ? AND enabled = TRUE;

-- GetEventAttendeeForUpdate reads a live attendee row under its row lock,
-- inside the transaction that changes its answer or edit grant.
-- name: GetEventAttendeeForUpdate :one
SELECT * FROM calendar_event_attendees
WHERE event_id = ? AND user_id = ? AND enabled = TRUE
FOR UPDATE;

-- name: SetEventAttendeeRsvp :exec
UPDATE calendar_event_attendees SET rsvp = ? WHERE event_id = ? AND user_id = ? AND enabled = TRUE;

-- name: SetEventAttendeeCanEdit :exec
UPDATE calendar_event_attendees SET can_edit = ? WHERE event_id = ? AND user_id = ? AND enabled = TRUE;
