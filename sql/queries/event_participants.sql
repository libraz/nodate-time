-- name: ListEventParticipants :many
SELECT ep.user_id, u.public_id AS user_public_id, u.name, u.icon, u.color
FROM event_participants ep
INNER JOIN users u ON u.id = ep.user_id
WHERE ep.event_id = ?
ORDER BY ep.created_at;

-- name: AddEventParticipant :exec
INSERT IGNORE INTO event_participants (event_id, user_id)
VALUES (?, ?);

-- name: DeleteAllEventParticipants :exec
DELETE FROM event_participants WHERE event_id = ?;
