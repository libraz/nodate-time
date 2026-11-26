-- Changes the default of users.week_start from 'mon' to 'sun' and moves the
-- rows that still hold 'mon' to 'sun'.
--
-- Rows are rewritten because a chosen 'mon' cannot be told apart from the old
-- default. The UPDATE comes before the ALTER: ALTER TABLE commits implicitly,
-- so stopping between them leaves the default at 'mon' and a re-run redoes
-- both. A column whose default is already 'sun' is left alone, so a re-run
-- does nothing. Run it with writers stopped.

SET NAMES utf8mb4;

DROP PROCEDURE IF EXISTS core_upgrade_0005;

DELIMITER //

CREATE PROCEDURE core_upgrade_0005()
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users'
      AND COLUMN_NAME = 'week_start'
      AND COLUMN_DEFAULT = 'mon'
  ) THEN
    UPDATE users SET week_start = 'sun', updated_at = updated_at
    WHERE week_start = 'mon';

    ALTER TABLE users ALTER COLUMN week_start SET DEFAULT 'sun';
  END IF;
END//

DELIMITER ;

CALL core_upgrade_0005();

DROP PROCEDURE core_upgrade_0005;
