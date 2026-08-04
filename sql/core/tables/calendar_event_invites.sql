-- ====================================
-- calendar_event_invites
-- Magic-link invite rows issued to calendar event attendees.
-- Each row carries a SHA-256 token hash and expiry so an unauthenticated
-- recipient can click the link, be identified as the intended attendee,
-- and update their RSVP. The plaintext token is never stored.
--
-- Lifecycle:
--   1. insert    : token_hash + expires_at set; sent_at / accepted_at NULL
--   2. dispatch  : sent_at set when the email is actually delivered
--   3. accept    : accepted_at set when the recipient opens the link
--   4. resend    : update the existing row in place (rotate token_hash,
--                  bump expires_at, clear sent_at / accepted_at) to respect
--                  the (event_id, attendee_id) uniqueness constraint.
-- ====================================
CREATE TABLE calendar_event_invites (
  id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  calendar_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to calendars.id',
  event_id INT UNSIGNED NULL COMMENT 'Internal FK to calendar_events.id; nullable so revoked invites survive event hard-delete (FK SET NULL)',
  attendee_id INT UNSIGNED NULL COMMENT 'Internal FK to calendar_event_attendees.id; nullable to mirror parent attendee being detached on event hard-delete',

  email VARCHAR(255) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Recipient email, denormalized from attendee for inbox queries',
  token_hash BINARY(32) NOT NULL COMMENT 'SHA-256 digest of the plaintext magic-link token',
  expires_at DATETIME(3) NOT NULL COMMENT 'Magic-link expiry',
  accepted_at DATETIME(3) NULL COMMENT 'Set when the recipient clicks the link',
  sent_at DATETIME(3) NULL COMMENT 'Set when the invite email is dispatched',

  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag; disabled rows are revoked invites',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_calendar_event_invites_public_id (public_id),
  UNIQUE KEY uniq_calendar_event_invites_workspace_public_id (workspace_id, public_id),
  UNIQUE KEY uniq_calendar_event_invites_token_hash (token_hash),
  UNIQUE KEY uniq_calendar_event_invites_event_attendee (event_id, attendee_id),
  KEY idx_calendar_event_invites_workspace_expires (workspace_id, expires_at),
  KEY idx_calendar_event_invites_workspace_email (workspace_id, email),
  KEY idx_calendar_event_invites_calendar_id (calendar_id),
  KEY idx_calendar_event_invites_attendee_id (attendee_id),

  CONSTRAINT fk_calendar_event_invites_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_invites_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE,
  CONSTRAINT fk_calendar_event_invites_event FOREIGN KEY (event_id) REFERENCES calendar_events(id) ON DELETE SET NULL,
  CONSTRAINT fk_calendar_event_invites_attendee FOREIGN KEY (attendee_id) REFERENCES calendar_event_attendees(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Magic-link invite rows for calendar event attendees';
