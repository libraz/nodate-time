#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

db_name="nodate_schema_upgrade_test"
db_port="${TC_DB_PORT:-33306}"
root_password="${TC_DB_ROOT_PASSWORD:-rootpw}"

mysql_client() {
  docker run --rm -i --network host \
    -e MYSQL_PWD="$root_password" \
    mysql:8.4 mysql --default-character-set=utf8mb4 \
    -h 127.0.0.1 -P "$db_port" -u root "$@"
}

cleanup() {
  mysql_client -e "DROP DATABASE IF EXISTS ${db_name}" >/dev/null
}
trap cleanup EXIT

mysql_client <<SQL
DROP DATABASE IF EXISTS ${db_name};
CREATE DATABASE ${db_name} CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
USE ${db_name};

CREATE TABLE users (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id BINARY(16) NOT NULL,
  name VARCHAR(100) NOT NULL,
  email VARCHAR(255) NOT NULL,
  icon VARCHAR(10) NOT NULL DEFAULT '👤',
  color VARCHAR(7) NOT NULL DEFAULT '#42A5F5',
  password_hash VARCHAR(255) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_public_id (public_id),
  UNIQUE KEY uk_users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE calendars (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id BINARY(16) NOT NULL,
  name VARCHAR(200) NOT NULL,
  color VARCHAR(7) NOT NULL DEFAULT '#42A5F5',
  cover_url VARCHAR(2000) NOT NULL DEFAULT '',
  created_by INT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_calendars_public_id (public_id),
  CONSTRAINT fk_calendars_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE events (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id BINARY(16) NOT NULL,
  calendar_id INT UNSIGNED NOT NULL,
  title VARCHAR(500) NOT NULL,
  all_day TINYINT(1) NOT NULL DEFAULT 0,
  start_at DATETIME(3) NOT NULL,
  end_at DATETIME(3) NOT NULL,
  color VARCHAR(7) NOT NULL DEFAULT '#42A5F5',
  location VARCHAR(500) NOT NULL DEFAULT '',
  memo TEXT NOT NULL,
  url VARCHAR(2000) NOT NULL DEFAULT '',
  created_by INT UNSIGNED NOT NULL,
  assigned_to INT UNSIGNED NULL,
  notification_offset INT NULL,
  recurrence_rule JSON NULL,
  recurrence_end DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_events_public_id (public_id),
  KEY idx_events_calendar_start (calendar_id, start_at),
  KEY idx_events_calendar_end (calendar_id, end_at),
  KEY idx_events_recurrence (calendar_id, recurrence_end),
  CONSTRAINT fk_events_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_events_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_events_assigned_to FOREIGN KEY (assigned_to) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE memos (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  public_id BINARY(16) NOT NULL,
  calendar_id INT UNSIGNED NOT NULL,
  title VARCHAR(500) NOT NULL,
  done TINYINT(1) NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0,
  created_by INT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_memos_public_id (public_id),
  KEY idx_memos_calendar (calendar_id, sort_order),
  CONSTRAINT fk_memos_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_memos_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE calendar_invites (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  calendar_id INT UNSIGNED NOT NULL,
  token VARCHAR(64) NOT NULL,
  role ENUM('admin', 'member', 'viewer') NOT NULL DEFAULT 'member',
  max_uses INT UNSIGNED NULL,
  use_count INT UNSIGNED NOT NULL DEFAULT 0,
  expires_at DATETIME(3) NULL,
  created_by INT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_invites_token (token),
  KEY idx_invites_calendar (calendar_id),
  CONSTRAINT fk_invites_calendar FOREIGN KEY (calendar_id) REFERENCES calendars (id) ON DELETE CASCADE,
  CONSTRAINT fk_invites_created_by FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE oauth_states (
  id INT UNSIGNED NOT NULL AUTO_INCREMENT,
  state_hash VARCHAR(64) NOT NULL,
  provider ENUM('google', 'line') NOT NULL,
  redirect VARCHAR(512) NOT NULL DEFAULT '',
  expires_at DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_os_state_hash (state_hash),
  KEY idx_os_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO users (public_id, name, email, password_hash)
VALUES (UNHEX('01800000000070008000000000000001'), 'Legacy User', 'legacy@example.com', 'hash');
INSERT INTO calendars (public_id, name, created_by)
VALUES (UNHEX('01800000000070008000000000000002'), 'Legacy Calendar', 1);
INSERT INTO events (public_id, calendar_id, title, start_at, end_at, memo, created_by)
VALUES (UNHEX('01800000000070008000000000000003'), 1, 'Legacy Event', '2026-01-01', '2026-01-02', '', 1);
INSERT INTO memos (public_id, calendar_id, title, created_by)
VALUES (UNHEX('01800000000070008000000000000004'), 1, 'Legacy Memo', 1);
INSERT INTO calendar_invites (calendar_id, token, created_by)
VALUES (1, 'legacy-token', 1);
INSERT INTO oauth_states (state_hash, provider, expires_at)
VALUES ('legacy-state', 'google', '2030-01-01');
SQL

bash sql/build-schema.sh >/dev/null
mysql_client "$db_name" < sql/schema.sql >/dev/null
mysql_client "$db_name" < sql/schema.sql >/dev/null

column_count="$(mysql_client --batch --skip-column-names "$db_name" -e "
  SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema = DATABASE() AND (
    (table_name = 'users' AND column_name IN ('avatar_storage_key','avatar_content_type','token_version','password_changed_at','is_admin')) OR
    (table_name = 'events' AND column_name IN ('timezone','recurrence_parent_id','recurrence_original_start','recurrence_cancelled')) OR
    (table_name = 'memos' AND column_name = 'body') OR
    (table_name = 'calendar_invites' AND column_name = 'is_public') OR
    (table_name = 'oauth_states' AND column_name IN ('code_verifier','nonce'))
  );")"
if [[ "$column_count" != "13" ]]; then
  echo "schema upgrade test: expected 13 compatibility columns, got $column_count" >&2
  exit 1
fi

structure_count="$(mysql_client --batch --skip-column-names "$db_name" -e "
  SELECT
    (SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'events' AND index_name = 'uk_events_recurrence_exception') +
    (SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema = DATABASE() AND table_name = 'events' AND constraint_name = 'fk_events_recurrence_parent');")"
if [[ "$structure_count" != "3" ]]; then
  echo "schema upgrade test: recurrence index or foreign key is missing" >&2
  exit 1
fi

defaults="$(mysql_client --batch --skip-column-names "$db_name" -e "
  SELECT CONCAT(e.timezone, ':', e.recurrence_cancelled, ':', u.token_version, ':', u.is_admin, ':', m.body, ':', i.is_public, ':', s.code_verifier, ':', s.nonce)
  FROM events e JOIN users u ON u.id = e.created_by JOIN memos m ON m.calendar_id = e.calendar_id
  JOIN calendar_invites i ON i.calendar_id = e.calendar_id CROSS JOIN oauth_states s LIMIT 1;")"
if [[ "$defaults" != "UTC:0:1:0::0::" ]]; then
  echo "schema upgrade test: legacy row defaults were not preserved: $defaults" >&2
  exit 1
fi

echo "Schema upgrade test passed"
