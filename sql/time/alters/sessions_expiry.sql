-- ====================================
-- sessions.expires_at — product-layer index for the expiry sweep
--
-- Cleanup collects every session past its expiry, a question that names no
-- user. The contract's index is (user_id, expires_at), and a composite
-- cannot be entered from its second column, so the sweep read the whole
-- table on every tick. The table grows with every sign-in the deployment
-- ever serves, which makes the cost of collecting it grow with use -- the
-- one thing a job on a timer must not do.
--
-- It lives here rather than in core/ because the sweep is this product's
-- policy. The contract records an expiry; it says nothing about who removes
-- the rows that have passed it, or when.
-- ====================================
ALTER TABLE sessions
  ADD KEY idx_sessions_expires_at (expires_at);
