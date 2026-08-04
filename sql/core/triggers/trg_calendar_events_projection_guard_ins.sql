-- ====================================
-- trg_calendar_events_projection_guard_ins
-- INSERT half of the calendar_events projection guard. See
-- trg_calendar_events_projection_guard_upd for the full rationale.
--
-- Enforces, unconditionally:
--   (task_id IS NULL) = (task_role IS NULL)
--   task_id IS NULL OR recurrence_rule IS NULL
--
-- and, unless @nf_item_projection_engine is set: a row may not be
-- created already linked to a task.
-- ====================================
DELIMITER $$

CREATE TRIGGER trg_calendar_events_projection_guard_ins
BEFORE INSERT ON calendar_events
FOR EACH ROW
BEGIN
  IF (NEW.task_id IS NULL) <> (NEW.task_role IS NULL) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'calendar_events.task_id and task_role must be set or cleared together';
  END IF;

  IF NEW.task_id IS NOT NULL AND NEW.recurrence_rule IS NOT NULL THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'a task-projected calendar event may not carry a recurrence rule';
  END IF;

  IF NEW.task_id IS NOT NULL AND @nf_item_projection_engine IS NULL THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'task-projected calendar events may only be written by the item projection engine';
  END IF;
END$$

DELIMITER ;
