-- The shape helper must fail when a rule expected to be stored is refused.
CALL nf_conformance_expect_recurrence('{"freq":"hourly"}', TRUE, '70 invalid rule expected to be stored');
