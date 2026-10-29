-- Workspace membership is a precondition of calendar membership
-- (PROTOCOL.md §Identity): an access check reads both, so a live
-- calendar_members row whose user has no live membership of the
-- calendar's workspace is a grant that should have gone with it. A
-- writer that removes someone from a workspace and leaves this behind
-- is relying on every reader to check the second table.

SET SESSION group_concat_max_len = 1048576;

SET @nf_cm_orphaned := (
  SELECT GROUP_CONCAT(CONCAT('calendar ', cm.calendar_id, ' user ', cm.user_id)
                      ORDER BY cm.calendar_id, cm.user_id SEPARATOR ', ')
  FROM calendar_members cm
  JOIN calendars c ON c.id = cm.calendar_id
  WHERE cm.enabled = TRUE
    AND NOT EXISTS (
      SELECT 1 FROM workspace_members wm
      WHERE wm.workspace_id = c.workspace_id
        AND wm.user_id = cm.user_id
        AND wm.enabled = TRUE));

CALL nf_conformance_assert(
  @nf_cm_orphaned IS NULL,
  CONCAT('90 live calendar members without a live membership of the calendar''s workspace: ',
         IFNULL(@nf_cm_orphaned, '')));

SET @nf_cm_orphaned = NULL;
