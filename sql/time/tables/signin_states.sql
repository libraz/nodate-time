-- ====================================
-- signin_states
-- Short-lived state for an OAuth sign-in that is still in flight.
--
-- Not the contract's oauth_states, which records a signed-in user linking
-- an external service and therefore requires a user_id. Here there is no
-- user yet: that is the whole point of the round trip. Reusing one table
-- would mean either making its user_id nullable for everyone, or inventing
-- a placeholder user for every abandoned login.
-- ====================================
CREATE TABLE signin_states (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',

  state_hash CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'SHA-256 hex of the state parameter. Hashed like every other bearer value here, so a database read cannot be replayed against the provider.',
  provider ENUM('google','github','microsoft','generic_oidc','line') NOT NULL COMMENT 'Provider the round trip is with',
  redirect_to VARCHAR(512) NULL COMMENT 'Where to send the browser after a successful sign-in. Validated against an allow-list before use: an unchecked value here is an open redirect.',
  code_verifier VARCHAR(128) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '' COMMENT 'PKCE verifier, exchanged for the code',
  nonce VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '' COMMENT 'OIDC nonce, checked against the id_token claim to bind the token to this request',
  expires_at DATETIME(3) NOT NULL COMMENT 'After this the round trip is abandoned',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_signin_states_public_id (public_id),
  UNIQUE KEY uniq_signin_states_state_hash (state_hash),
  KEY idx_signin_states_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='In-flight OAuth sign-in state';
