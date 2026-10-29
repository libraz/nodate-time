-- Every rule in sql/core/recurrence/fixtures.json is stored by the shape
-- CHECK. The fixtures are what the expanders are tested against, so a
-- rule the database refuses is one no writer could ever have persisted,
-- and its expected dates prove nothing about real rows.
--
-- run.sh loads the file into @nf_recurrence_fixtures.

DROP PROCEDURE IF EXISTS nf_conformance_insert_fixture_rules;

DELIMITER $$

CREATE PROCEDURE nf_conformance_insert_fixture_rules()
BEGIN
  DECLARE done BOOLEAN DEFAULT FALSE;
  DECLARE fixture_name VARCHAR(255);
  DECLARE fixture_rule TEXT;
  DECLARE cur CURSOR FOR
    SELECT jt.name, CAST(jt.rule AS CHAR)
    FROM JSON_TABLE(@nf_recurrence_fixtures, '$[*]'
           COLUMNS (name VARCHAR(255) PATH '$.name', rule JSON PATH '$.rule')) jt;
  DECLARE CONTINUE HANDLER FOR NOT FOUND SET done = TRUE;

  SET @nf_fixture_rules_seen := 0;
  OPEN cur;
  read_loop: LOOP
    FETCH cur INTO fixture_name, fixture_rule;
    IF done THEN
      LEAVE read_loop;
    END IF;
    CALL nf_conformance_assert(fixture_rule IS NOT NULL,
      CONCAT('fixture ', IFNULL(fixture_name, '(unnamed)'), ': has no rule'));
    CALL nf_conformance_expect_recurrence(fixture_rule, TRUE,
      CONCAT('fixture ', IFNULL(fixture_name, '(unnamed)')));
    SET @nf_fixture_rules_seen := @nf_fixture_rules_seen + 1;
  END LOOP;
  CLOSE cur;
END$$

DELIMITER ;

CALL nf_conformance_assert(
  JSON_TYPE(@nf_recurrence_fixtures) = 'ARRAY' AND JSON_LENGTH(@nf_recurrence_fixtures) > 0,
  'fixture file is not a non-empty JSON array');

CALL nf_conformance_insert_fixture_rules();

CALL nf_conformance_assert(
  @nf_fixture_rules_seen = JSON_LENGTH(@nf_recurrence_fixtures),
  CONCAT('fixture rules inserted ', @nf_fixture_rules_seen, ' of ', JSON_LENGTH(@nf_recurrence_fixtures)));

DROP PROCEDURE IF EXISTS nf_conformance_insert_fixture_rules;
SET @nf_fixture_rules_seen = NULL;
SET @nf_recurrence_fixtures = NULL;
