-- A failure message longer than MESSAGE_TEXT holds must still report,
-- with its leading text intact.
CALL nf_conformance_assert(FALSE, CONCAT('00 long message ', REPEAT('x', 400)));
