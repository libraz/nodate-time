-- The shape helper must fail when a rule expected to be refused is stored.
CALL nf_conformance_expect_recurrence('{"freq":"daily"}', FALSE, '70 valid rule expected to be refused');
