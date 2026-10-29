-- ====================================
-- storage_blob_removals
-- Object-storage keys whose blob is still to be removed after the rows
-- that named it were deleted. A workspace or user teardown cascades away
-- the storage_objects rows behind its blobs, so it writes each key here
-- in the same transaction: a removal that fails or never runs leaves this
-- record instead of an unnamed orphan. The storage sweeper removes the
-- blob and then this row, every pass, until the removal succeeds.
-- ====================================
CREATE TABLE storage_blob_removals (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  storage_key VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Object key whose blob is still to be removed from object storage',

  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_storage_blob_removals_storage_key (storage_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Pending object-storage blob removals whose referencing rows are already deleted';
