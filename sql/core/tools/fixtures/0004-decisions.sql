-- The operator's decisions for the base rows no rule settles (groups 10
-- and 11). core_upgrade_0004_decisions exists once 0004 has run and stopped.

INSERT INTO core_upgrade_0004_decisions (public_id, form, zone) VALUES
  (UNHEX('01900000000070008000000000000112'), 'nt', NULL),
  (UNHEX('01900000000070008000000000000113'), 'nt', 'Asia/Tokyo');
