-- Every calendar.* and workspace.* row in the event log names a kind some
-- event-kinds registry present in this deployment declares
-- (PROTOCOL.md §Event kinds). Readers skip kinds they do not know, so an
-- undeclared or misspelt one is not an error anywhere else: it is a
-- change every other product silently never sees.
--
-- run.sh fills nf_registered_kinds from the `- kind:` lines of every
-- sql/<layer>/event-kinds.yaml beside this suite. The comparison is on
-- bytes, so a kind differing only in case is not mistaken for the
-- declared one.

CALL nf_conformance_assert(
  (SELECT COUNT(*) > 0 FROM nf_registered_kinds),
  '80 no event kinds were loaded from the registries; the check would accept nothing as undeclared');

SET @nf_ek_unregistered := (
  SELECT GROUP_CONCAT(DISTINCT e.type ORDER BY e.type SEPARATOR ', ')
  FROM events e
  WHERE (e.type LIKE 'calendar.%' OR e.type LIKE 'workspace.%')
    AND NOT EXISTS (
      SELECT 1 FROM nf_registered_kinds r WHERE r.kind = CAST(e.type AS BINARY)));

CALL nf_conformance_assert(
  @nf_ek_unregistered IS NULL,
  CONCAT('80 events rows carry kinds no registry declares: ', IFNULL(@nf_ek_unregistered, '')));

SET @nf_ek_unregistered = NULL;
