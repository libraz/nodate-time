-- ====================================
-- trg_calendar_events_projection_guard_del
-- DELETE half of the calendar_events projection guard. See
-- trg_calendar_events_projection_guard_upd for the full rationale.
--
-- Nothing in the product hard-deletes a calendar event: removal is
-- enabled = FALSE, which the UPDATE guard already covers. This trigger
-- exists for the writer that reaches past the product and issues a real
-- DELETE, which would drop a task's projection with no compensating
-- row in `events` and no way to notice afterwards.
--
-- Workspace and calendar teardown are unaffected: MySQL does not
-- activate triggers for rows removed by a foreign key referential
-- action.
-- ====================================
DELIMITER $$

CREATE TRIGGER trg_calendar_events_projection_guard_del
BEFORE DELETE ON calendar_events
FOR EACH ROW
BEGIN
  IF OLD.task_id IS NOT NULL AND @nf_item_projection_engine IS NULL THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'task-projected calendar events may only be deleted by the item projection engine';
  END IF;
END$$

DELIMITER ;
