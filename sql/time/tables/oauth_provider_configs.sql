-- ====================================
-- oauth_provider_configs
-- Per-provider OAuth client credentials, editable from the admin panel.
--
-- The contract's identities table records which provider authenticated a
-- user; it says nothing about how this deployment talks to that provider.
-- That is operator configuration and lives here. It is deliberately not
-- instance_settings: the client secret needs its own encrypted column
-- rather than a TEXT value alongside settings meant to be read in the
-- clear.
-- ====================================
CREATE TABLE oauth_provider_configs (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',

  provider ENUM('google','github','microsoft','generic_oidc','line') NOT NULL COMMENT 'Provider this configuration is for. Overlaps identities.provider but is not a subset of it: a provider can be configured before anyone has signed in with it, and local has no configuration.',
  client_id VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL DEFAULT '' COMMENT 'OAuth client identifier',
  client_secret_ciphertext VARBINARY(512) NOT NULL COMMENT 'Encrypted client secret (AES-256-GCM). Never returned by the API.',
  updated_by_user_id INT UNSIGNED NULL COMMENT 'Last operator to change it',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether this provider is offered at sign-in',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_oauth_provider_configs_public_id (public_id),
  UNIQUE KEY uniq_oauth_provider_configs_provider (provider),

  CONSTRAINT fk_oauth_provider_configs_updater FOREIGN KEY (updated_by_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Per-provider OAuth client configuration';
