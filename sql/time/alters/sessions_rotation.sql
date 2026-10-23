-- ====================================
-- sessions — remember when the refresh token was last exchanged
--
-- The retired hash itself is the contract's rotated_from_hash, and replay
-- detection reads only that column. What a second presentation of it implies
-- is this product's policy: the session is closed, unless the exchange that
-- retired it was seconds ago, which reads as one client asking twice. That
-- grace window needs the time of the last exchange, which the contract does
-- not keep, so it lives here.
-- ====================================
ALTER TABLE sessions
  ADD COLUMN rotated_at DATETIME(3) NULL COMMENT 'When the refresh token was last exchanged';
