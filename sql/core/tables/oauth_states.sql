-- ====================================
-- oauth_states
-- Short-lived CSRF-protection state tokens for the personal OAuth
-- flow (POST /me/integrations/{provider}/connect → GET /oauth/
-- callback/{provider}). A row is inserted when the user clicks
-- "Connect", and deleted atomically when the callback handler
-- consumes the matching state. Rows expire after 15 minutes; the
-- callback refuses stale rows.
-- ====================================
CREATE TABLE oauth_states (
  state CHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL PRIMARY KEY COMMENT 'Random 32-byte token, hex-encoded',

  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id — the user who started the connect flow',
  provider ENUM('github','slack','google_calendar','discord') NOT NULL COMMENT 'Which provider this state belongs to. ''discord'' is required for the personal Discord presence-binding flow (Phase 8 presence-discord gateway).',
  redirect_to VARCHAR(512) NULL COMMENT 'Optional client-supplied return URL to send the user to after the callback completes',

  expires_at DATETIME(3) NOT NULL COMMENT 'Hard expiry; callback handler rejects rows past this timestamp',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  KEY idx_oauth_states_expires_at (expires_at),

  CONSTRAINT fk_oauth_states_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Short-lived OAuth CSRF state tokens for personal integrations';
