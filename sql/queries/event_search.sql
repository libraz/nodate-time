-- SearchEventCandidates reads the rows a text search may match for one member,
-- across every calendar they belong to in the workspace.
--
-- Title is searched where the member may read it (a party, or an event that
-- is public or private); location and memo only where they may read those (a
-- party, or a public event), so a hit never reveals a field the reader is
-- not shown. A party is the owner, the creator or an enabled attendee, with
-- an override's own attendees replacing the series' once it has any rows.
--
-- Rows that can never produce an upcoming occurrence are left out here: a
-- one-off whose end is not after from_at (all-day rows compare against
-- from_day, the caller's local date as a UTC midnight) and a series that has
-- ended. The caller applies the exact per-occurrence test.
-- name: SearchEventCandidates :many
SELECT e.* FROM calendar_events e
INNER JOIN calendars c ON c.id = e.calendar_id AND c.enabled = TRUE AND c.workspace_id = sqlc.arg(workspace_id)
INNER JOIN calendar_members cm ON cm.calendar_id = c.id AND cm.enabled = TRUE AND cm.user_id = sqlc.arg(user_id)
LEFT JOIN calendar_events p ON p.id = e.recurrence_parent_id
WHERE e.enabled = TRUE
  AND e.start_at IS NOT NULL
  AND e.end_at IS NOT NULL
  AND (e.recurrence_parent_id IS NULL OR p.enabled = TRUE)
  AND (
    (e.recurrence_rule IS NOT NULL
      AND (e.recurrence_end IS NULL
        OR e.recurrence_end >= LEAST(CAST(sqlc.arg(from_at) AS DATETIME(3)), CAST(sqlc.arg(from_day) AS DATETIME(3)))))
    OR (e.recurrence_rule IS NULL
      AND (e.end_at > IF(e.all_day, CAST(sqlc.arg(from_day) AS DATETIME(3)), CAST(sqlc.arg(from_at) AS DATETIME(3)))
        OR (e.end_at = e.start_at
          AND e.end_at >= IF(e.all_day, CAST(sqlc.arg(from_day) AS DATETIME(3)), CAST(sqlc.arg(from_at) AS DATETIME(3))))))
  )
  AND (
    (
      (e.owner_user_id = sqlc.arg(user_id)
        OR e.created_by_user_id = sqlc.arg(user_id)
        OR EXISTS (
          SELECT 1 FROM calendar_event_attendees a
          WHERE a.user_id = sqlc.arg(user_id) AND a.enabled = TRUE
            AND a.event_id = IF(
              e.recurrence_parent_id IS NOT NULL
                AND NOT EXISTS (SELECT 1 FROM calendar_event_attendees own WHERE own.event_id = e.id),
              e.recurrence_parent_id, e.id)
        )
        OR IF(e.visibility = 'default', c.default_event_visibility, e.visibility) IN ('public', 'private'))
      AND e.title COLLATE utf8mb4_0900_ai_ci LIKE CAST(sqlc.arg(pattern) AS CHAR) ESCAPE '\\'
    )
    OR (
      (e.owner_user_id = sqlc.arg(user_id)
        OR e.created_by_user_id = sqlc.arg(user_id)
        OR EXISTS (
          SELECT 1 FROM calendar_event_attendees a
          WHERE a.user_id = sqlc.arg(user_id) AND a.enabled = TRUE
            AND a.event_id = IF(
              e.recurrence_parent_id IS NOT NULL
                AND NOT EXISTS (SELECT 1 FROM calendar_event_attendees own WHERE own.event_id = e.id),
              e.recurrence_parent_id, e.id)
        )
        OR IF(e.visibility = 'default', c.default_event_visibility, e.visibility) = 'public')
      AND (e.location COLLATE utf8mb4_0900_ai_ci LIKE CAST(sqlc.arg(pattern) AS CHAR) ESCAPE '\\' OR e.memo COLLATE utf8mb4_0900_ai_ci LIKE CAST(sqlc.arg(pattern) AS CHAR) ESCAPE '\\')
    )
  )
ORDER BY e.id
LIMIT ?;

-- ListSearchSeriesByIDs reads the series that candidate overrides belong to.
-- name: ListSearchSeriesByIDs :many
SELECT * FROM calendar_events
WHERE id IN (sqlc.slice('series_ids')) AND enabled = TRUE;
