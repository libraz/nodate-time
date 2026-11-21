-- ListEventsOwnedBy names the events a departing member owns on a calendar,
-- read before they are handed on so each can be logged with its title. Series
-- masters and single events only: an override is part of its series and is
-- reassigned with it.
-- name: ListEventsOwnedBy :many
SELECT id, public_id, title
FROM calendar_events
WHERE calendar_id = ? AND owner_user_id = ? AND enabled = TRUE
  AND recurrence_parent_id IS NULL
ORDER BY id;
