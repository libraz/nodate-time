-- Replaces the users.theme_preference families aurora / dotline / glass with
-- glass / modern / washi / classic / nothing.
--
-- aurora-X becomes glass-X and dotline-X becomes modern-X; glass-X is kept.
-- The column is widened to the union of the old and new values, the rows are
-- rewritten, then the column is narrowed to the new set. A column already in
-- the new shape is left alone, so the file can be run again.

SET NAMES utf8mb4;

DROP PROCEDURE IF EXISTS core_upgrade_0003;

DELIMITER //

CREATE PROCEDURE core_upgrade_0003()
BEGIN
  DECLARE v_new VARCHAR(512) DEFAULT
    'enum(''glass-light'',''glass-dark'',''modern-light'',''modern-dark'',''washi-light'',''washi-dark'',''classic-light'',''classic-dark'',''nothing-light'',''nothing-dark'',''system'')';

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users'
      AND COLUMN_NAME = 'theme_preference'
      AND COLUMN_TYPE <> v_new
  ) THEN
    ALTER TABLE users MODIFY COLUMN theme_preference
      ENUM('aurora-light','aurora-dark','dotline-light','dotline-dark',
           'glass-light','glass-dark','modern-light','modern-dark',
           'washi-light','washi-dark','classic-light','classic-dark',
           'nothing-light','nothing-dark','system')
      NOT NULL DEFAULT 'system' COMMENT 'UI theme preference';

    UPDATE users
    SET theme_preference = CASE theme_preference
          WHEN 'aurora-light' THEN 'glass-light'
          WHEN 'aurora-dark' THEN 'glass-dark'
          WHEN 'dotline-light' THEN 'modern-light'
          WHEN 'dotline-dark' THEN 'modern-dark'
        END,
        updated_at = updated_at
    WHERE theme_preference IN ('aurora-light', 'aurora-dark', 'dotline-light', 'dotline-dark');

    ALTER TABLE users MODIFY COLUMN theme_preference
      ENUM('glass-light','glass-dark','modern-light','modern-dark',
           'washi-light','washi-dark','classic-light','classic-dark',
           'nothing-light','nothing-dark','system')
      NOT NULL DEFAULT 'system' COMMENT 'UI theme preference';
  END IF;
END//

DELIMITER ;

CALL core_upgrade_0003();

DROP PROCEDURE core_upgrade_0003;
