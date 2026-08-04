-- ====================================
-- trg_calendar_events_projection_guard_upd
-- Keeps a task-projected calendar event in step with the task it
-- projects, from the database rather than from application code.
--
-- Two classes of rule live here.
--
-- Unconditional shape invariants. These would be CHECK constraints,
-- but MySQL 8.4+ forbids a CHECK that references a column used in a
-- foreign key referential action, and task_id carries ON DELETE SET
-- NULL. A trigger has no such restriction:
--
--   (task_id IS NULL) = (task_role IS NULL)
--   task_id IS NULL OR recurrence_rule IS NULL
--   (recurrence_parent_id IS NULL) = (recurrence_original_start IS NULL)
--   recurrence_parent_id IS NULL OR recurrence_rule IS NULL
--   recurrence_parent_id IS NULL OR task_id IS NULL
--
-- Ownership of the projection. task_id / task_role and the columns
-- that mirror a task field (title, start_at, end_at, and the
-- enabled soft-delete flag) may only be written by the projection
-- engine, which moves the task and the event together inside one
-- transaction and appends the matching item.* row to `events`. A
-- writer that changes them directly desyncs the pair and emits no
-- domain event, which is exactly the drift the reconciler reports as
-- item_inconsistency_total.
--
-- Columns with no task-side counterpart (memo, location, visibility,
-- show_as, url, block_label, ...) stay freely editable on a projected
-- row: they carry no invariant, so guarding them would only make the
-- calendar UI lie about what it can offer.
--
-- The engine opts in by setting @nf_item_projection_engine = 1 on the
-- connection before its write and clearing it afterwards; the variable
-- is session-scoped, so a fresh connection is always guarded. In a
-- deployment without the flow layer task_id is always NULL and none of
-- these branches can fire.
--
-- Comparisons use <=> because start_at / end_at / task_id are nullable
-- and `<>` yields NULL — never true — as soon as either side is NULL.
-- ====================================
DELIMITER $$

CREATE TRIGGER trg_calendar_events_projection_guard_upd
BEFORE UPDATE ON calendar_events
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

  IF (NEW.recurrence_parent_id IS NULL) <> (NEW.recurrence_original_start IS NULL) THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'calendar_events.recurrence_parent_id and recurrence_original_start must be set or cleared together';
  END IF;

  IF NEW.recurrence_parent_id IS NOT NULL AND NEW.recurrence_rule IS NOT NULL THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'an occurrence override may not carry a recurrence rule of its own';
  END IF;

  IF NEW.recurrence_parent_id IS NOT NULL AND NEW.task_id IS NOT NULL THEN
    SIGNAL SQLSTATE '45000'
      SET MESSAGE_TEXT = 'a task-projected calendar event may not be an occurrence override';
  END IF;

  IF @nf_item_projection_engine IS NULL THEN
    IF NOT (NEW.task_id <=> OLD.task_id) OR NOT (NEW.task_role <=> OLD.task_role) THEN
      SIGNAL SQLSTATE '45000'
        SET MESSAGE_TEXT = 'the task projection link may only be changed by the item projection engine';
    END IF;

    IF OLD.task_id IS NOT NULL
       AND (NOT (NEW.title <=> OLD.title)
         OR NOT (NEW.start_at <=> OLD.start_at)
         OR NOT (NEW.end_at <=> OLD.end_at)
         OR NOT (NEW.enabled <=> OLD.enabled)) THEN
      SIGNAL SQLSTATE '45000'
        SET MESSAGE_TEXT = 'title, start_at, end_at and enabled mirror the linked task and may only be written by the item projection engine';
    END IF;
  END IF;
END$$

DELIMITER ;
