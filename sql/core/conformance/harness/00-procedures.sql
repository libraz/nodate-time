-- Conformance harness: assertion helpers plus the fixtures the schema
-- checks operate on.
--
-- Assertions raise SQLSTATE 45000. The mysql client exits non-zero on the
-- first error, so the runner needs no result parsing: a clean exit is a
-- pass and any output is the failure.

DROP PROCEDURE IF EXISTS nf_conformance_assert;
DROP PROCEDURE IF EXISTS nf_conformance_expect_rejected;

DELIMITER $$

-- nf_conformance_assert fails the run when `ok` is not true. A NULL is
-- treated as a failure rather than a pass, so a mistyped column name in
-- the caller's expression surfaces instead of silently succeeding.
CREATE PROCEDURE nf_conformance_assert(IN ok BOOLEAN, IN what TEXT)
BEGIN
  IF ok IS NULL OR NOT ok THEN
    SET @nf_conformance_msg = CONCAT('conformance failure: ', what);
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

DELIMITER ;
