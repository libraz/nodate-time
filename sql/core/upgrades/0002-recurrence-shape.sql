-- Brings every calendar_events.recurrence_rule to the canonical shape and
-- then adds chk_calendar_events_recurrence_shape, which enforces it.
--
-- Rewrites that keep a rule's meaning run first: a scalar byMonthDay or
-- bySetPos becomes a one-element array, keys holding null, "" or an empty
-- array are removed (each means the same as an absent key), an interval below 1 becomes 1, count 0 is removed, and a JSON
-- null rule becomes SQL NULL. A rule that still fails the shape after that
-- cannot be repaired without guessing, so the run lists those rows by
-- public_id and stops before the constraint is added. Fix or clear them
-- (NULL makes the event a single occurrence) and run the file again.
--
-- Every step is a no-op on a rule already in shape, so the file can be run
-- again. updated_at is kept as it was on every rewritten row.

SET NAMES utf8mb4;

DROP PROCEDURE IF EXISTS core_upgrade_0002;

DELIMITER //

CREATE PROCEDURE core_upgrade_0002()
BEGIN
  -- Byte-identical to the literal in sql/core/tables/calendar_events.sql.
  DECLARE v_schema TEXT DEFAULT '{"type":"object","additionalProperties":false,"required":["freq"],"properties":{"freq":{"enum":["daily","weekly","monthly","yearly"]},"interval":{"type":"integer","minimum":1,"maximum":999},"byDay":{"type":"array","minItems":1,"maxItems":7,"uniqueItems":true,"items":{"enum":["MO","TU","WE","TH","FR","SA","SU"]}},"byMonthDay":{"type":"array","minItems":1,"maxItems":31,"uniqueItems":true,"items":{"type":"integer","minimum":1,"maximum":31}},"bySetPos":{"type":"array","minItems":1,"maxItems":1,"items":{"enum":[1,2,3,4,5,-1]}},"wkst":{"enum":["MO","SU"]},"until":{"type":"string","minLength":10,"maxLength":40},"count":{"type":"integer","minimum":1,"maximum":1000}},"dependencies":{"bySetPos":["byDay"]},"not":{"required":["until","count"]}}';
  DECLARE v_key VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin;
  DECLARE v_path TEXT;
  DECLARE v_bad_count INT;
  DECLARE v_bad_ids TEXT;
  DECLARE v_msg VARCHAR(128);

  -- 1. A scalar byMonthDay / bySetPos becomes a one-element array. A JSON
  --    null is left for step 2, which removes the key instead.
  UPDATE calendar_events
  SET recurrence_rule = JSON_SET(recurrence_rule, '$.byMonthDay',
                                 JSON_ARRAY(JSON_EXTRACT(recurrence_rule, '$.byMonthDay'))),
      updated_at = updated_at
  WHERE JSON_TYPE(recurrence_rule) = 'OBJECT'
    AND JSON_TYPE(JSON_EXTRACT(recurrence_rule, '$.byMonthDay')) NOT IN ('ARRAY', 'OBJECT', 'NULL');

  UPDATE calendar_events
  SET recurrence_rule = JSON_SET(recurrence_rule, '$.bySetPos',
                                 JSON_ARRAY(JSON_EXTRACT(recurrence_rule, '$.bySetPos'))),
      updated_at = updated_at
  WHERE JSON_TYPE(recurrence_rule) = 'OBJECT'
    AND JSON_TYPE(JSON_EXTRACT(recurrence_rule, '$.bySetPos')) NOT IN ('ARRAY', 'OBJECT', 'NULL');

  -- 2. Values that carry no meaning, and values with an equivalent in range.
  --    A JSON null rule means no recurrence, as SQL NULL does.
  UPDATE calendar_events
  SET recurrence_rule = NULL, updated_at = updated_at
  WHERE JSON_TYPE(recurrence_rule) = 'NULL';

  -- Every key any rule carries, so a null, "" or [] under an unknown key is
  -- removed too. A key containing a quote or backslash cannot be spelled
  -- as a path here; it stays, and step 3 reports the row.
  DROP TEMPORARY TABLE IF EXISTS core_upgrade_0002_keys;
  CREATE TEMPORARY TABLE core_upgrade_0002_keys (
    k VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL PRIMARY KEY
  ) ENGINE=InnoDB;

  INSERT IGNORE INTO core_upgrade_0002_keys (k)
  SELECT jt.k
  FROM calendar_events ce,
       JSON_TABLE(JSON_KEYS(ce.recurrence_rule), '$[*]' COLUMNS (k VARCHAR(255) PATH '$')) jt
  WHERE JSON_TYPE(ce.recurrence_rule) = 'OBJECT'
    AND LOCATE('"', jt.k) = 0
    AND LOCATE('\\', jt.k) = 0;

  SET v_key = (SELECT MIN(k) FROM core_upgrade_0002_keys);
  WHILE v_key IS NOT NULL DO
    SET v_path = CONCAT('$."', v_key, '"');
    UPDATE calendar_events
    SET recurrence_rule = JSON_REMOVE(recurrence_rule, v_path),
        updated_at = updated_at
    WHERE JSON_TYPE(recurrence_rule) = 'OBJECT'
      AND (JSON_TYPE(JSON_EXTRACT(recurrence_rule, v_path)) = 'NULL'
           OR (JSON_TYPE(JSON_EXTRACT(recurrence_rule, v_path)) = 'STRING'
               AND JSON_UNQUOTE(JSON_EXTRACT(recurrence_rule, v_path)) = '')
           OR (JSON_TYPE(JSON_EXTRACT(recurrence_rule, v_path)) = 'ARRAY'
               AND JSON_LENGTH(JSON_EXTRACT(recurrence_rule, v_path)) = 0));
    SET v_key = (SELECT MIN(k) FROM core_upgrade_0002_keys WHERE k > v_key);
  END WHILE;

  DROP TEMPORARY TABLE core_upgrade_0002_keys;

  UPDATE calendar_events
  SET recurrence_rule = JSON_SET(recurrence_rule, '$.interval', 1),
      updated_at = updated_at
  WHERE JSON_TYPE(recurrence_rule) = 'OBJECT'
    AND JSON_TYPE(JSON_EXTRACT(recurrence_rule, '$.interval')) IN ('INTEGER', 'UNSIGNED INTEGER')
    AND JSON_EXTRACT(recurrence_rule, '$.interval') < 1;

  UPDATE calendar_events
  SET recurrence_rule = JSON_REMOVE(recurrence_rule, '$.count'),
      updated_at = updated_at
  WHERE JSON_TYPE(recurrence_rule) = 'OBJECT'
    AND JSON_TYPE(JSON_EXTRACT(recurrence_rule, '$.count')) IN ('INTEGER', 'UNSIGNED INTEGER')
    AND JSON_EXTRACT(recurrence_rule, '$.count') = 0;

  -- 3. Whatever still fails the shape needs a person. The full list goes
  --    out as a result set; the error carries the count and as many ids as
  --    MESSAGE_TEXT holds.
  SELECT COUNT(*),
         GROUP_CONCAT(BIN_TO_UUID(public_id) ORDER BY id SEPARATOR ' ')
    INTO v_bad_count, v_bad_ids
  FROM calendar_events
  WHERE recurrence_rule IS NOT NULL
    AND NOT JSON_SCHEMA_VALID(v_schema, recurrence_rule);

  IF v_bad_count > 0 THEN
    SELECT BIN_TO_UUID(public_id) AS invalid_rule_public_id, recurrence_rule
    FROM calendar_events
    WHERE recurrence_rule IS NOT NULL
      AND NOT JSON_SCHEMA_VALID(v_schema, recurrence_rule)
    ORDER BY id;

    SET v_msg = LEFT(CONCAT('0002: ', v_bad_count, ' recurrence_rule rows fail the shape: ', v_bad_ids), 128);
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = v_msg;
  END IF;

  -- 4. The constraint. DDL takes no variables, so the literal is spliced
  --    into a prepared statement; the schema holds no single quote.
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'calendar_events'
      AND CONSTRAINT_TYPE = 'CHECK'
      AND CONSTRAINT_NAME = 'chk_calendar_events_recurrence_shape'
  ) THEN
    SET @core_upgrade_0002_ddl = CONCAT(
      'ALTER TABLE calendar_events ADD CONSTRAINT chk_calendar_events_recurrence_shape ',
      'CHECK (recurrence_rule IS NULL OR JSON_SCHEMA_VALID(''', v_schema, ''', recurrence_rule))');
    PREPARE core_upgrade_0002_stmt FROM @core_upgrade_0002_ddl;
    EXECUTE core_upgrade_0002_stmt;
    DEALLOCATE PREPARE core_upgrade_0002_stmt;
    SET @core_upgrade_0002_ddl = NULL;
  END IF;
END//

DELIMITER ;

CALL core_upgrade_0002();

DROP PROCEDURE core_upgrade_0002;
