-- name: ListEventAttendees :many
SELECT a.user_id, a.rsvp, a.can_edit, u.public_id AS user_public_id,
       u.display_name, u.avatar_url
FROM calendar_event_attendees a
INNER JOIN users u ON u.id = a.user_id
WHERE a.event_id = ? AND a.enabled = TRUE
ORDER BY a.created_at;

-- AddEventAttendee revives a removed row rather than inserting beside it,
-- so re-adding somebody does not leave two rows for one person.
-- name: AddEventAttendee :exec
INSERT INTO calendar_event_attendees (public_id, workspace_id, event_id, user_id)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE enabled = TRUE;

-- name: SetEventAttendeeRsvp :exec
UPDATE calendar_event_attendees SET rsvp = ? WHERE event_id = ? AND user_id = ?;

-- name: RemoveAllEventAttendees :exec
UPDATE calendar_event_attendees SET enabled = FALSE WHERE event_id = ?;
