-- ====================================
-- calendar_event_attendees
-- Event participants with RSVP state and per-attendee edit permission.
-- The event owner can grant can_edit to specific attendees, enabling
-- collaborative editing without giving full calendar manager access.
-- ====================================
CREATE TABLE calendar_event_attendees (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  event_id INT UNSIGNED NULL COMMENT 'Internal FK to calendar_events.id; nullable so audit-trail attendee rows survive event hard-delete (FK SET NULL); active rows for live events are NOT NULL via app constraint',
  user_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to users.id',
  /**
   * event_id_key: de-NULLed projection of event_id used to power the UNIQUE
   * key over (event_id, user_id). MySQL UNIQUE indexes treat NULLs as
   * distinct, so without this surrogate two audit-trail rows with
   * event_id IS NULL (typical after the parent event is hard-deleted with
   * ON DELETE SET NULL) for the same user_id would silently coexist,
   * defeating the (event_id, user_id) invariant for live rows. The 0
   * sentinel is safe because event_id references calendar_events.id, an
   * AUTO_INCREMENT column whose values start at 1 — 0 cannot collide with
   * any real event row.
   *
   * Must be VIRTUAL (not STORED): event_id is the FK target of an
   * ON DELETE SET NULL action against calendar_events. MySQL refuses to
   * create that FK while a STORED generated column depends on event_id
   * and is declared NOT NULL, because the SET NULL action would have to
   * physically rewrite the NOT NULL stored column. VIRTUAL avoids the
   * physical write — the value is recomputed at read / index-update time,
   * and MySQL 8.0+ supports indexes (incl. UNIQUE) on virtual columns,
   * so uniq_calendar_event_attendees_event_user keeps working unchanged.
   * IFNULL(event_id, 0) is deterministic, satisfying the indexed-virtual
   * column requirement.
   */
  event_id_key INT UNSIGNED GENERATED ALWAYS AS (IFNULL(event_id, 0)) VIRTUAL NOT NULL COMMENT 'De-NULLed surrogate for event_id; 0 when event_id IS NULL. Exists solely to power uniq_calendar_event_attendees_event_user over (event_id_key, user_id). VIRTUAL (not STORED) so the FK ON DELETE SET NULL on event_id can be created — STORED + NOT NULL would fail the FK precondition check at table creation time.',

  rsvp ENUM('pending','accepted','declined','tentative') NOT NULL DEFAULT 'pending' COMMENT 'Attendance response',
  can_edit BOOLEAN NOT NULL DEFAULT FALSE COMMENT 'Whether this attendee can edit the event (granted by owner)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_event_attendees_public_id (public_id),
  UNIQUE KEY uniq_calendar_event_attendees_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_calendar_event_attendees_event_user (event_id_key, user_id),
  KEY idx_calendar_event_attendees_event_user (event_id, user_id),
  KEY idx_calendar_event_attendees_workspace_user (workspace_id, user_id),

  CONSTRAINT fk_calendar_event_attendees_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_attendees_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE SET NULL,
  CONSTRAINT fk_calendar_event_attendees_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Calendar event attendees with RSVP and edit permission';
