-- Rows only a non-conforming identity writer leaves (PROTOCOL.md
-- §Identity).

SET SESSION group_concat_max_len = 1048576;

-- Every session names the application that issued it. An application
-- refreshes and purges only its own audience, so a row with none is one
-- no application will ever refresh, look up or clean up.
SET @nf_id_no_audience := (
  SELECT GROUP_CONCAT(s.id ORDER BY s.id SEPARATOR ', ')
  FROM sessions s
  WHERE s.audience = '');

CALL nf_conformance_assert(
  @nf_id_no_audience IS NULL,
  CONCAT('95 sessions with an empty audience, by id: ', IFNULL(@nf_id_no_audience, '')));

-- Addresses are stored lower-cased. The column's collation would match
-- them either way, but the sign-in allowlist compares bytes, so an
-- upper-case address fails a match that should succeed.
SET @nf_id_mixed_case := (
  SELECT GROUP_CONCAT(u.id ORDER BY u.id SEPARATOR ', ')
  FROM users u
  WHERE CAST(u.email AS BINARY) <> CAST(LOWER(u.email) AS BINARY));

CALL nf_conformance_assert(
  @nf_id_mixed_case IS NULL,
  CONCAT('95 users whose email is not lower-case, by id: ', IFNULL(@nf_id_mixed_case, '')));

SET @nf_id_no_audience = NULL;
SET @nf_id_mixed_case = NULL;
