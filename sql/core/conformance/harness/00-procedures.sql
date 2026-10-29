-- Conformance harness: assertion helpers plus the fixtures the schema
-- checks operate on.
--
-- Assertions raise SQLSTATE 45000. The mysql client exits non-zero on the
-- first error, so the runner needs no result parsing: a clean exit is a
-- pass and any output is the failure.

DROP PROCEDURE IF EXISTS nf_conformance_assert;
DROP PROCEDURE IF EXISTS nf_conformance_expect_rejected;
DROP PROCEDURE IF EXISTS nf_conformance_expect_duplicate;
DROP PROCEDURE IF EXISTS nf_conformance_expect_recurrence;

DELIMITER $$

-- nf_conformance_assert fails the run when `ok` is not true. A NULL is
-- treated as a failure rather than a pass, so a mistyped column name in
-- the caller's expression surfaces instead of silently succeeding.
--
-- MESSAGE_TEXT holds 128 characters and a longer one is itself an error
-- that hides the failure, so the full text is printed as a result row and
-- the signal carries its first 128 characters. Callers put the check
-- number and the offending names first, where truncation cannot reach.
CREATE PROCEDURE nf_conformance_assert(IN ok BOOLEAN, IN what TEXT)
BEGIN
  IF ok IS NULL OR NOT ok THEN
    SET @nf_conformance_msg = CONCAT('conformance failure: ', IFNULL(what, '(no description)'));
    SELECT @nf_conformance_msg AS conformance_failure;
    SET @nf_conformance_msg = LEFT(@nf_conformance_msg, 128);
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = @nf_conformance_msg;
  END IF;
END$$

-- nf_conformance_expect_rejected runs `stmt` and fails the run unless the
-- database refuses it with SQLSTATE 45000 — the state every guard trigger
-- signals with. Any other error propagates, so a rejection for an
-- unrelated reason (a missing column, a foreign key) is reported rather
-- than counted as the guard working.
--
-- The handler lives in a nested block so it goes out of scope before the
-- assertion below it. Left in the outer scope it would catch the
-- assertion's own SIGNAL and turn a failed check into a silent pass.
CREATE PROCEDURE nf_conformance_expect_rejected(IN stmt TEXT, IN what TEXT)
BEGIN
  DECLARE rejected BOOLEAN DEFAULT FALSE;

  BEGIN
    DECLARE CONTINUE HANDLER FOR SQLSTATE '45000' SET rejected = TRUE;
    SET @nf_conformance_stmt = stmt;
    PREPARE nf_conformance_ps FROM @nf_conformance_stmt;
    EXECUTE nf_conformance_ps;
    DEALLOCATE PREPARE nf_conformance_ps;
  END;

  CALL nf_conformance_assert(rejected, what);
END$$

-- nf_conformance_expect_duplicate is the same shape as
-- nf_conformance_expect_rejected but watches for SQLSTATE 23000, which
-- is what a unique index reports. The two are separate procedures
-- because the distinction is the point: a guard trigger refusing a write
-- and an index refusing it are different mechanisms, and a check that
-- accepted either would pass while the constraint it names is missing.
CREATE PROCEDURE nf_conformance_expect_duplicate(IN stmt TEXT, IN what TEXT)
BEGIN
  DECLARE rejected BOOLEAN DEFAULT FALSE;

  BEGIN
    DECLARE CONTINUE HANDLER FOR SQLSTATE '23000' SET rejected = TRUE;
    SET @nf_conformance_stmt = stmt;
    PREPARE nf_conformance_ps FROM @nf_conformance_stmt;
    EXECUTE nf_conformance_ps;
    DEALLOCATE PREPARE nf_conformance_ps;
  END;

  CALL nf_conformance_assert(rejected, what);
END$$

-- nf_conformance_expect_recurrence inserts an event carrying `rule` and
-- fails the run unless chk_calendar_events_recurrence_shape gives the
-- expected answer: refused when `accepted` is false, stored when true.
-- A refusal by any other constraint is a failure in both directions, so a
-- rule that only fails for an unrelated reason never reads as the shape
-- check working. Needs the fixture variables @ws, @cal and @usr; stored
-- rows land in the fixture workspace and leave with it.
CREATE PROCEDURE nf_conformance_expect_recurrence(IN rule TEXT, IN accepted BOOLEAN, IN what TEXT)
BEGIN
  DECLARE refused BOOLEAN DEFAULT FALSE;
  DECLARE other_error TEXT DEFAULT NULL;
  DECLARE err_text TEXT DEFAULT NULL;

  BEGIN
    DECLARE CONTINUE HANDLER FOR 3819
    BEGIN
      GET DIAGNOSTICS CONDITION 1 err_text = MESSAGE_TEXT;
      IF err_text LIKE '%chk_calendar_events_recurrence_shape%' THEN
        SET refused = TRUE;
      ELSE
        SET other_error = err_text;
      END IF;
    END;
    INSERT INTO calendar_events
      (public_id, workspace_id, calendar_id, title, start_at, end_at, timezone,
       owner_user_id, created_by_user_id, recurrence_rule)
    VALUES
      (UUID_TO_BIN(UUID(), 0), @ws, @cal, 'recurrence shape', '2030-02-01 09:00:00',
       '2030-02-01 10:00:00', 'UTC', @usr, @usr, CAST(rule AS JSON));
  END;

  CALL nf_conformance_assert(other_error IS NULL,
    CONCAT(what, ': refused by another constraint: ', IFNULL(other_error, '')));
  CALL nf_conformance_assert(refused <> accepted,
    CONCAT(what, IF(accepted, ': the shape CHECK refused ', ': the shape CHECK accepted '),
           IFNULL(rule, 'NULL')));
END$$

DELIMITER ;
