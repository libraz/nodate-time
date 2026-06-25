-- Append-only history of create/update/delete on shared content (events, memos).
-- Intentionally decoupled from the target rows (no FK to events/memos) so a
-- deleted item's history — including who deleted it — is preserved.
CREATE TABLE IF NOT EXISTS audit_log (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  calendar_id      INT UNSIGNED    NOT NULL,
  entity_type      VARCHAR(16)     NOT NULL COMMENT 'event | memo',
  entity_id        INT UNSIGNED    NOT NULL COMMENT 'internal id of the target row (may no longer exist)',
  entity_public_id BINARY(16)      NOT NULL COMMENT 'public id of the target, stable across deletion',
  action           VARCHAR(16)     NOT NULL COMMENT 'create | update | delete',
  actor_id         INT UNSIGNED    NULL     COMMENT 'user who performed the action',
  summary          VARCHAR(500)    NOT NULL DEFAULT '' COMMENT 'snapshot of the target title at action time',
  created_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_audit_entity (entity_type, entity_public_id, created_at),
  KEY idx_audit_calendar (calendar_id, created_at),
  CONSTRAINT fk_audit_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_audit_actor FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
