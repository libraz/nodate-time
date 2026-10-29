-- ====================================
-- oauth_signin_allowlist
-- Instance-level opt-in allowlist of who may sign in through OAuth/OIDC.
-- NOT workspace-scoped: sign-in happens before any workspace is chosen, so
-- an entry admits a person to the deployment, not to a tenant.
--
-- The list is opt-in: with no enabled row every verified email may sign in,
-- which is the default open behaviour. With at least one enabled row an
-- address is admitted when it matches an 'email' entry outright, or when the
-- part after its final '@' matches a 'domain' entry. Entries configured
-- through NF_OAUTH_ALLOWED_DOMAINS / NF_OAUTH_ALLOWED_EMAILS remain a floor
-- the operator can always set; rows here are what an instance administrator
-- edits at runtime, and the two are unioned.
-- ====================================
CREATE TABLE oauth_signin_allowlist (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  added_by_user_id INT UNSIGNED NULL COMMENT 'Internal FK to users.id (who added the entry, null once that account is gone)',

  entry_kind ENUM('domain','email') NOT NULL COMMENT 'What entry_value names: domain matches the part after the final ''@'', email matches the whole address',
  entry_value VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_bin NOT NULL COMMENT 'The domain or address this entry admits, normalized to lower case before it is written; latin1_bin keeps every comparison byte-exact, so cafe.example and café.example are distinct entries rather than one',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag — FALSE withdraws the entry without releasing its (entry_kind, entry_value) claim, so re-adding it revives this row via ON DUPLICATE KEY UPDATE',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_oauth_signin_allowlist_public_id (public_id),
  UNIQUE KEY uniq_oauth_signin_allowlist_kind_value (entry_kind, entry_value),

  CONSTRAINT fk_oauth_signin_allowlist_added_by
    FOREIGN KEY (added_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Instance-level OAuth/OIDC sign-in allowlist';
