-- ====================================
-- avatar_uploads
-- Reservations for in-flight avatar uploads.
--
-- A row is created when a presigned URL is handed out and removed once
-- the upload is claimed. It is not the avatar: the finished object is
-- storage_objects, referenced from users.avatar_storage_object_id. This
-- table exists so an upload that is started and abandoned leaves a
-- record with an expiry, which is what the sweeper deletes by.
-- ====================================
CREATE TABLE avatar_uploads (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',

  storage_key VARCHAR(1000) NOT NULL COMMENT 'Object key the presigned URL was issued for',
  content_type VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Declared MIME type',
  byte_size BIGINT UNSIGNED NOT NULL COMMENT 'Declared size in bytes',
  expires_at DATETIME(3) NOT NULL COMMENT 'After this the reservation is abandoned and the object may be swept',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_avatar_uploads_public_id (public_id),
  KEY idx_avatar_uploads_user_expiry (user_id, expires_at),
  KEY idx_avatar_uploads_expiry (expires_at),

  CONSTRAINT fk_avatar_uploads_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='In-flight avatar upload reservations';
