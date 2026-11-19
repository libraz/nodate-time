-- Current recipients for a calendar. The extra row makes an over-limit
-- roster an explicit error instead of silently omitting people from a poll.
-- name: ListSchedulePollCalendarMembers :many
SELECT cm.user_id, u.public_id, u.display_name, cm.role
FROM calendar_members cm
JOIN workspace_members wm ON wm.workspace_id = cm.workspace_id AND wm.user_id = cm.user_id AND wm.enabled = TRUE
JOIN users u ON u.id = cm.user_id AND u.enabled = TRUE
WHERE cm.workspace_id = ? AND cm.calendar_id = ? AND cm.enabled = TRUE
ORDER BY u.display_name, cm.user_id
LIMIT ?;

-- name: ListSchedulePollsVisibleByCalendar :many
SELECT p.* FROM calendar_schedule_polls p
WHERE p.workspace_id = ? AND p.calendar_id = ?
  AND (
    EXISTS (
      SELECT 1 FROM calendar_schedule_poll_members pm
      JOIN calendar_members cm ON cm.workspace_id = p.workspace_id AND cm.calendar_id = p.calendar_id
        AND cm.user_id = pm.user_id AND cm.enabled = TRUE
      JOIN workspace_members wm ON wm.workspace_id = p.workspace_id AND wm.user_id = pm.user_id AND wm.enabled = TRUE
      WHERE pm.poll_id = p.id AND pm.user_id = ? AND pm.enabled = TRUE
    )
    OR EXISTS (
      SELECT 1 FROM calendar_members actor_cm
      JOIN workspace_members actor_wm ON actor_wm.workspace_id = actor_cm.workspace_id
        AND actor_wm.user_id = actor_cm.user_id AND actor_wm.enabled = TRUE
      WHERE actor_cm.workspace_id = p.workspace_id AND actor_cm.calendar_id = p.calendar_id
        AND actor_cm.user_id = ? AND actor_cm.enabled = TRUE
        AND actor_cm.role IN ('owner','manager')
        AND (
          p.manager_user_id = ?
          OR (p.status = 'open' AND NOT EXISTS (
            SELECT 1 FROM calendar_members assigned_cm
            JOIN workspace_members assigned_wm ON assigned_wm.workspace_id = assigned_cm.workspace_id
              AND assigned_wm.user_id = assigned_cm.user_id AND assigned_wm.enabled = TRUE
            JOIN users assigned_user ON assigned_user.id = assigned_cm.user_id AND assigned_user.enabled = TRUE
            WHERE assigned_cm.workspace_id = p.workspace_id AND assigned_cm.calendar_id = p.calendar_id
              AND assigned_cm.user_id = p.manager_user_id AND assigned_cm.enabled = TRUE
              AND assigned_cm.role IN ('owner','manager')
          ))
        )
    )
  )
ORDER BY p.created_at DESC, p.id DESC
LIMIT ?;

-- name: GetSchedulePollByPublicID :one
SELECT * FROM calendar_schedule_polls
WHERE workspace_id = ? AND calendar_id = ? AND public_id = ?;

-- name: GetSchedulePollForUpdateByPublicID :one
SELECT * FROM calendar_schedule_polls
WHERE workspace_id = ? AND calendar_id = ? AND public_id = ?
FOR UPDATE;

-- name: IsSchedulePollManagerEligible :one
SELECT EXISTS (
  SELECT 1 FROM calendar_schedule_polls p
  JOIN calendar_members cm ON cm.workspace_id = p.workspace_id AND cm.calendar_id = p.calendar_id
    AND cm.user_id = p.manager_user_id AND cm.enabled = TRUE AND cm.role IN ('owner','manager')
  JOIN workspace_members wm ON wm.workspace_id = p.workspace_id AND wm.user_id = cm.user_id AND wm.enabled = TRUE
  JOIN users u ON u.id = cm.user_id AND u.enabled = TRUE
  WHERE p.id = ? AND p.manager_user_id IS NOT NULL
) AS eligible;

-- name: CreateSchedulePoll :execresult
INSERT INTO calendar_schedule_polls
  (public_id, workspace_id, calendar_id, created_by_user_id, manager_user_id, title, timezone)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSchedulePollOrganizer :exec
UPDATE calendar_schedule_polls
SET title = ?, manager_user_id = ?, version = version + 1,
    response_version = response_version + ?
WHERE id = ?;

-- name: AdvanceSchedulePollVersion :exec
UPDATE calendar_schedule_polls
SET version = version + 1, response_version = response_version + ?
WHERE id = ?;

-- name: AdvanceSchedulePollReviewVersion :exec
UPDATE calendar_schedule_polls SET response_version = response_version + 1
WHERE id = ?;

-- name: SetSchedulePollFinalized :exec
UPDATE calendar_schedule_polls
SET status = 'finalized', selected_slot_public_id = ?, confirmed_event_public_id = ?, version = version + 1
WHERE id = ? AND status = 'open';

-- name: UpsertSchedulePollAnswer :exec
INSERT INTO calendar_schedule_poll_answers (poll_id, slot_id, member_id, user_id, value)
VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE value = VALUES(value);

-- name: SetSchedulePollCancelled :exec
UPDATE calendar_schedule_polls
SET status = 'cancelled', version = version + 1
WHERE id = ? AND status = 'open';

-- name: InsertSchedulePollMember :exec
INSERT INTO calendar_schedule_poll_members (poll_id, user_id, enabled)
VALUES (?, ?, TRUE)
ON DUPLICATE KEY UPDATE enabled = TRUE;

-- name: SetSchedulePollMemberEnabled :exec
UPDATE calendar_schedule_poll_members SET enabled = ?
WHERE poll_id = ? AND user_id = ?;

-- name: AdvanceSchedulePollMemberAnswerVersion :exec
UPDATE calendar_schedule_poll_members SET answer_version = answer_version + 1
WHERE poll_id = ? AND id = ? AND enabled = TRUE;

-- name: AdvanceSchedulePollAnswerVersions :exec
UPDATE calendar_schedule_poll_members SET answer_version = answer_version + 1
WHERE poll_id = ? AND enabled = TRUE;

-- name: GetSchedulePollMemberByPollAndUser :one
SELECT * FROM calendar_schedule_poll_members
WHERE poll_id = ? AND user_id = ?;

-- name: ListSchedulePollMembers :many
SELECT * FROM calendar_schedule_poll_members
WHERE poll_id = ?
ORDER BY user_id;

-- name: ListSchedulePollMembersCurrent :many
SELECT pm.id, pm.user_id, u.public_id, u.display_name
FROM calendar_schedule_poll_members pm
JOIN calendar_schedule_polls p ON p.id = pm.poll_id
JOIN calendar_members cm ON cm.workspace_id = p.workspace_id AND cm.calendar_id = p.calendar_id
  AND cm.user_id = pm.user_id AND cm.enabled = TRUE
JOIN workspace_members wm ON wm.workspace_id = p.workspace_id AND wm.user_id = pm.user_id AND wm.enabled = TRUE
JOIN users u ON u.id = pm.user_id AND u.enabled = TRUE
WHERE pm.poll_id = ? AND pm.enabled = TRUE
ORDER BY u.display_name, pm.user_id;

-- name: ListSchedulePollSlots :many
SELECT * FROM calendar_schedule_poll_slots
WHERE poll_id = ?
ORDER BY start_at, end_at, id;

-- name: GetSchedulePollSlotByPublicID :one
SELECT * FROM calendar_schedule_poll_slots
WHERE poll_id = ? AND public_id = ?;

-- name: InsertSchedulePollSlot :execresult
INSERT INTO calendar_schedule_poll_slots (public_id, poll_id, start_at, end_at, availability, enabled)
VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateSchedulePollSlotAvailability :exec
UPDATE calendar_schedule_poll_slots
SET availability = ?, enabled = ?
WHERE id = ?;

-- name: DisableSchedulePollSlot :exec
UPDATE calendar_schedule_poll_slots SET enabled = FALSE
WHERE poll_id = ? AND id = ? AND enabled = TRUE;

-- name: DeleteSchedulePollSlotAnswers :exec
DELETE FROM calendar_schedule_poll_answers WHERE poll_id = ? AND slot_id = ?;

-- name: DeleteSchedulePollMemberAnswers :exec
DELETE FROM calendar_schedule_poll_answers WHERE poll_id = ? AND member_id = ?;

-- name: ListSchedulePollAnswersForUser :many
SELECT s.public_id AS slot_public_id, a.value
FROM calendar_schedule_poll_answers a
JOIN calendar_schedule_poll_slots s ON s.id = a.slot_id AND s.poll_id = a.poll_id AND s.enabled = TRUE
JOIN calendar_schedule_poll_members pm ON pm.id = a.member_id AND pm.poll_id = a.poll_id AND pm.enabled = TRUE
WHERE a.poll_id = ? AND pm.user_id = ?
ORDER BY s.start_at, s.id;

-- name: ListSchedulePollAnswersForManager :many
SELECT pm.user_id, u.public_id AS member_public_id, s.public_id AS slot_public_id, a.value
FROM calendar_schedule_poll_answers a
JOIN calendar_schedule_poll_slots s ON s.id = a.slot_id AND s.poll_id = a.poll_id AND s.enabled = TRUE
JOIN calendar_schedule_poll_members pm ON pm.id = a.member_id AND pm.poll_id = a.poll_id AND pm.enabled = TRUE
JOIN calendar_schedule_polls p ON p.id = pm.poll_id
JOIN calendar_members cm ON cm.workspace_id = p.workspace_id AND cm.calendar_id = p.calendar_id
  AND cm.user_id = pm.user_id AND cm.enabled = TRUE
JOIN workspace_members wm ON wm.workspace_id = p.workspace_id AND wm.user_id = pm.user_id AND wm.enabled = TRUE
JOIN users u ON u.id = pm.user_id AND u.enabled = TRUE
WHERE a.poll_id = ?
ORDER BY u.display_name, s.start_at, s.id;

-- name: ListSchedulePollActionPolls :many
SELECT p.id, p.public_id, p.calendar_id, p.workspace_id, p.manager_user_id,
       p.title, p.timezone, p.status, p.version, p.response_version, p.created_at,
       c.public_id AS calendar_public_id, c.name AS calendar_name
FROM calendar_schedule_polls p
JOIN calendars c ON c.id = p.calendar_id AND c.workspace_id = p.workspace_id AND c.enabled = TRUE
JOIN calendar_members actor_cm ON actor_cm.workspace_id = p.workspace_id
  AND actor_cm.calendar_id = p.calendar_id AND actor_cm.user_id = ? AND actor_cm.enabled = TRUE
JOIN workspace_members actor_wm ON actor_wm.workspace_id = p.workspace_id
  AND actor_wm.user_id = actor_cm.user_id AND actor_wm.enabled = TRUE
WHERE p.workspace_id = ? AND p.status = 'open'
  AND (p.manager_user_id = ? OR EXISTS (
    SELECT 1 FROM calendar_schedule_poll_members pm
    WHERE pm.poll_id = p.id AND pm.user_id = ? AND pm.enabled = TRUE
  ))
ORDER BY p.created_at DESC, p.id DESC
LIMIT ?;

-- name: CountSchedulePollCurrentMembers :one
SELECT COUNT(*) AS member_count
FROM calendar_schedule_poll_members pm
JOIN calendar_schedule_polls p ON p.id = pm.poll_id
JOIN calendar_members cm ON cm.workspace_id = p.workspace_id AND cm.calendar_id = p.calendar_id
  AND cm.user_id = pm.user_id AND cm.enabled = TRUE
JOIN workspace_members wm ON wm.workspace_id = p.workspace_id AND wm.user_id = pm.user_id AND wm.enabled = TRUE
JOIN users u ON u.id = pm.user_id AND u.enabled = TRUE
WHERE pm.poll_id = ? AND pm.enabled = TRUE;

-- name: CountSchedulePollCurrentResponded :one
SELECT COUNT(*) AS responded_count
FROM calendar_schedule_poll_members pm
JOIN calendar_schedule_polls p ON p.id = pm.poll_id
JOIN calendar_members cm ON cm.workspace_id = p.workspace_id AND cm.calendar_id = p.calendar_id
  AND cm.user_id = pm.user_id AND cm.enabled = TRUE
JOIN workspace_members wm ON wm.workspace_id = p.workspace_id AND wm.user_id = pm.user_id AND wm.enabled = TRUE
JOIN users u ON u.id = pm.user_id AND u.enabled = TRUE
WHERE pm.poll_id = ? AND pm.enabled = TRUE
  AND EXISTS (SELECT 1 FROM calendar_schedule_poll_slots s WHERE s.poll_id = pm.poll_id AND s.enabled = TRUE)
  AND NOT EXISTS (
    SELECT 1 FROM calendar_schedule_poll_slots s
    WHERE s.poll_id = pm.poll_id AND s.enabled = TRUE
      AND NOT EXISTS (SELECT 1 FROM calendar_schedule_poll_answers a
        WHERE a.poll_id = pm.poll_id AND a.slot_id = s.id AND a.member_id = pm.id)
  );

-- name: CountSchedulePollMemberPendingSlots :one
SELECT COUNT(*) AS pending_count
FROM calendar_schedule_poll_slots s
WHERE s.poll_id = ? AND s.enabled = TRUE
  AND NOT EXISTS (SELECT 1 FROM calendar_schedule_poll_answers a
    WHERE a.poll_id = s.poll_id AND a.slot_id = s.id AND a.member_id = ?);
