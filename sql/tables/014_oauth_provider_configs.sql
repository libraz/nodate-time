CREATE TABLE IF NOT EXISTS oauth_provider_configs (
  provider               VARCHAR(32)    NOT NULL,
  client_id              VARCHAR(255)   NOT NULL DEFAULT '',
  client_secret_enc      VARBINARY(512) NOT NULL,
  enabled                BOOLEAN        NOT NULL DEFAULT TRUE,
  updated_at             DATETIME(3)    NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  updated_by             INT UNSIGNED   NULL,
  PRIMARY KEY (provider),
  CONSTRAINT fk_opc_user FOREIGN KEY (updated_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
