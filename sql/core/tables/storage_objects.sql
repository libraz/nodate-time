-- ====================================
-- storage_objects
-- Content-addressed metadata index for blobs that physically live in MinIO
-- (S3-compatible object storage). One row per unique (scope, sha256) tuple
-- so that uploading the same file twice within a scope reuses the underlying
-- object and only bumps ref_count.
--
-- Scope is exclusive: a row either belongs to a workspace (task / calendar
-- event attachments) or to a single user (avatar). The check constraint
-- enforces exactly one of workspace_id / owner_user_id is non-null.
--
-- Lifecycle: creating a referencing row (attachments / users.avatar_*) bumps
-- ref_count, deleting one decrements it. A background sweeper hard-deletes
-- the MinIO object and this row when ref_count reaches 0. FKs from referrers
-- use ON DELETE RESTRICT so we never lose track of a still-referenced blob.
-- ====================================
CREATE TABLE storage_objects (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NULL COMMENT 'Internal FK to workspaces.id; non-null for workspace-scoped blobs (task/calendar attachments). Mutually exclusive with owner_user_id.',
  owner_user_id INT UNSIGNED NULL COMMENT 'Internal FK to users.id; non-null for user-scoped blobs (avatar). Mutually exclusive with workspace_id.',

  sha256 BINARY(32) NOT NULL COMMENT 'SHA-256 of the raw blob; basis for content addressing and dedup within a scope',
  byte_size BIGINT UNSIGNED NOT NULL COMMENT 'Size in bytes of the underlying blob',
  content_type VARCHAR(127) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'MIME type recorded at upload time',
  storage_key VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Computed object key in MinIO (e.g. workspace/{wsPublicId}/{sha256_hex} or user/{userPublicId}/{sha256_hex})',
  ref_count INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Number of referencing rows (attachments / users.avatar_storage_object_id); GC eligible when 0',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_storage_objects_public_id (public_id),
  -- Per-scope content dedup. NULLs are distinct in MySQL UNIQUE indexes, so
  -- the workspace-scope and user-scope rows do not interfere with each other.
  UNIQUE KEY uniq_storage_objects_workspace_sha (workspace_id, sha256),
  UNIQUE KEY uniq_storage_objects_user_sha (owner_user_id, sha256),
  UNIQUE KEY uniq_storage_objects_storage_key (storage_key),

  CONSTRAINT fk_storage_objects_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_storage_objects_owner_user FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT chk_storage_objects_scope_exclusive CHECK (
    (workspace_id IS NOT NULL AND owner_user_id IS NULL) OR
    (workspace_id IS NULL     AND owner_user_id IS NOT NULL)
  )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Content-addressed object storage references; rows shared across attachments via storage_object_id with ref_count GC.';
