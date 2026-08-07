-- ====================================
-- sessions — remember the refresh hash that was just spent
--
-- Rotation overwrites the stored hash, which leaves nothing behind that could
-- recognise a second use of the token it replaced. A replayed refresh token
-- then looks exactly like a random string: both match no row, both get the
-- same refusal, and a token that leaked is spent in silence.
--
-- Keeping the retired hash is what makes the replay nameable. A refresh
-- request matching it is, by construction, presenting a token this session
-- already traded in — either the legitimate client retrying an exchange whose
-- reply it never saw, or somebody else holding a copy. Nothing distinguishes
-- the two, so the session is closed and both sides sign in again.
--
-- It lives here rather than in core/ because what a retired token means is
-- this product's policy. The contract stores a hash; it does not say what a
-- second presentation of a spent one implies.
-- ====================================
ALTER TABLE sessions
  ADD COLUMN prev_refresh_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'SHA-256 hex of the refresh token this session last traded in; presenting it again is a replay',
  ADD COLUMN rotated_at DATETIME(3) NULL COMMENT 'When the refresh token was last exchanged',
  ADD KEY idx_sessions_prev_refresh_hash (prev_refresh_hash);
