-- Rows in the shape v0.1.0 (and main before the core upgrades) wrote them,
-- one or more for every rewrite sql/core/upgrades and
-- sql/time/upgrades/from-v0.1.0.sql perform. Loaded by
-- scripts/check-upgrade-path.sh into each starting schema, so every INSERT
-- names only columns both starting states have.

SET NAMES utf8mb4;
SET time_zone = '+00:00';

INSERT INTO workspaces (id, public_id, slug, name, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000001'), 'home', 'Home',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

-- Mixed-case addresses are lower-cased.
INSERT INTO users (id, public_id, email, display_name, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000a1'), 'Owner@Example.COM', 'Owner',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-0000000000a2'), 'member@example.com', 'Member',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO workspace_members (id, public_id, workspace_id, user_id, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000101'), 1, 1, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-000000000102'), 1, 2, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendars (id, public_id, workspace_id, name, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000c1'), 1, 'Family',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendar_members (id, public_id, workspace_id, calendar_id, user_id, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000201'), 1, 1, 1, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-000000000202'), 1, 1, 2, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

-- Calendar members the workspace does not back: user 3 has no workspace row
-- at all (in two calendars, so it gains exactly one), user 4 was removed from
-- the workspace and keeps a live calendar membership.
INSERT INTO users (id, public_id, email, display_name, created_at, updated_at) VALUES
  (3, UUID_TO_BIN('01900000-0000-7000-8000-0000000000a3'), 'invited@example.com', 'Invited',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (4, UUID_TO_BIN('01900000-0000-7000-8000-0000000000a4'), 'removed@example.com', 'Removed',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO workspace_members (id, public_id, workspace_id, user_id, enabled, created_at, updated_at) VALUES
  (4, UUID_TO_BIN('01900000-0000-7000-8000-000000000104'), 1, 4, FALSE, '2026-01-01 00:00:00.000', '2026-01-02 00:00:00.000');

INSERT INTO calendars (id, public_id, workspace_id, name, created_at, updated_at) VALUES
  (2, UUID_TO_BIN('01900000-0000-7000-8000-0000000000c2'), 1, 'Club',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendar_members (id, public_id, workspace_id, calendar_id, user_id, created_at, updated_at) VALUES
  (3, UUID_TO_BIN('01900000-0000-7000-8000-000000000203'), 1, 1, 3, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (4, UUID_TO_BIN('01900000-0000-7000-8000-000000000204'), 1, 2, 3, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (5, UUID_TO_BIN('01900000-0000-7000-8000-000000000205'), 1, 1, 4, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

-- Recurrence rules in every old shape the upgrades repair:
--   1 weekly with no wkst (gains SU), 2 scalar byMonthDay, 3 scalar bySetPos
--   and a null-valued key, 4 interval 0 and count 0, 5 already canonical.
INSERT INTO calendar_events
  (id, public_id, workspace_id, calendar_id, title, start_at, end_at, recurrence_rule,
   owner_user_id, created_by_user_id, enabled, created_at, updated_at)
VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000e1'), 1, 1, 'Standup',
   '2026-01-06 09:00:00.000', '2026-01-06 09:30:00.000',
   '{"freq":"weekly","interval":2,"byDay":["TU","SU"]}',
   1, 1, TRUE, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-0000000000e2'), 1, 1, 'Rent',
   '2026-01-31 10:00:00.000', '2026-01-31 11:00:00.000',
   '{"freq":"monthly","byMonthDay":31}',
   1, 1, TRUE, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (3, UUID_TO_BIN('01900000-0000-7000-8000-0000000000e3'), 1, 1, 'Book club',
   '2026-01-12 19:00:00.000', '2026-01-12 21:00:00.000',
   '{"freq":"monthly","byDay":["MO"],"bySetPos":2,"until":null}',
   1, 1, TRUE, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (4, UUID_TO_BIN('01900000-0000-7000-8000-0000000000e4'), 1, 1, 'Walk',
   '2026-01-01 07:00:00.000', '2026-01-01 07:30:00.000',
   '{"freq":"daily","interval":0,"count":0}',
   1, 1, TRUE, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (5, UUID_TO_BIN('01900000-0000-7000-8000-0000000000e5'), 1, 1, 'Old plan',
   '2026-01-05 12:00:00.000', '2026-01-05 13:00:00.000',
   '{"freq":"weekly","byDay":["MO"],"wkst":"MO","count":3}',
   1, 1, FALSE, '2026-01-01 00:00:00.000', '2026-01-02 00:00:00.000'),
  (6, UUID_TO_BIN('01900000-0000-7000-8000-0000000000e6'), 1, 1, 'Dentist',
   '2026-02-03 15:00:00.000', '2026-02-03 16:00:00.000', NULL,
   1, 1, TRUE, '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendar_memos (id, public_id, workspace_id, calendar_id, created_by_user_id, title, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000d1'), 1, 1, 1, 'Buy milk',
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendar_invites (id, public_id, workspace_id, calendar_id, created_by_user_id, token_hash, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000f1'), 1, 1, 1, REPEAT('a', 64),
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

INSERT INTO calendar_event_comments (id, public_id, workspace_id, event_id, author_id, body, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000001c1'), 1, 1, 2, 'see you there',
   '2026-01-02 00:00:00.000', '2026-01-02 00:00:00.000');

-- Storage. v0.1.0 counted only confirmed, live references, so every count
-- below except the avatar's is wrong against "rows pointing at the object":
--   1 photo (0, should be 1)        2 thumbnail under the photo (3, should be 1)
--   3 attachment reservation (0, 1) 4 deleted attachment (0, 1)
--   5 avatar (1, correct)           6 nothing points at it (2, should be 0)
INSERT INTO storage_objects
  (id, public_id, workspace_id, owner_user_id, sha256, byte_size, content_type, storage_key,
   ref_count, uploaded_at, created_at, updated_at)
VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000501'), 1, NULL, UNHEX(SHA2('photo', 256)), 100,
   'image/jpeg', 'albums/01900000-0000-7000-8000-0000000000b1', 0,
   '2026-01-03 00:00:00.000', '2026-01-03 00:00:00.000', '2026-01-03 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-000000000502'), 1, NULL, UNHEX(SHA2('thumb', 256)), 10,
   'image/jpeg', 'albums/01900000-0000-7000-8000-0000000000b1/thumb', 3,
   '2026-01-03 00:00:00.000', '2026-01-03 00:00:00.000', '2026-01-03 00:00:00.000'),
  (3, UUID_TO_BIN('01900000-0000-7000-8000-000000000503'), 1, NULL, UNHEX(SHA2('reserved', 256)), 200,
   'application/pdf', 'workspace/01900000-0000-7000-8000-000000000001/reserved', 0,
   NULL, '2026-01-04 00:00:00.000', '2026-01-04 00:00:00.000'),
  (4, UUID_TO_BIN('01900000-0000-7000-8000-000000000504'), 1, NULL, UNHEX(SHA2('agenda', 256)), 300,
   'application/pdf', 'workspace/01900000-0000-7000-8000-000000000001/agenda', 0,
   '2026-01-04 00:00:00.000', '2026-01-04 00:00:00.000', '2026-01-05 00:00:00.000'),
  (5, UUID_TO_BIN('01900000-0000-7000-8000-000000000505'), NULL, 1, UNHEX(SHA2('avatar', 256)), 50,
   'image/png', 'user/01900000-0000-7000-8000-0000000000a1/avatar', 1,
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (6, UUID_TO_BIN('01900000-0000-7000-8000-000000000506'), 1, NULL, UNHEX(SHA2('orphan', 256)), 400,
   'text/plain', 'workspace/01900000-0000-7000-8000-000000000001/orphan', 2,
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

UPDATE users SET avatar_storage_object_id = 5, updated_at = updated_at WHERE id = 1;

-- An unconfirmed reservation (updated_at = created_at) and a confirmed
-- attachment that was later deleted (updated_at moved on); only the second
-- is marked retired.
INSERT INTO calendar_event_attachments
  (id, public_id, workspace_id, event_id, uploader_id, storage_object_id, filename, enabled, created_at, updated_at)
VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000001a1'), 1, 1, 1, 3, 'draft.pdf', FALSE,
   '2026-01-04 00:00:00.000', '2026-01-04 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-0000000001a2'), 1, 1, 1, 4, 'agenda.pdf', FALSE,
   '2026-01-04 00:00:00.000', '2026-01-05 00:00:00.000');

-- A live photo with its thumbnail, and a deleted one from before the
-- storage backfill.
INSERT INTO album_photos
  (id, public_id, workspace_id, calendar_id, uploaded_by_user_id, caption, storage_key,
   storage_object_id, thumbnail_object_id, enabled, created_at, updated_at)
VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-0000000000b1'), 1, 1, 1, 'Beach',
   'albums/01900000-0000-7000-8000-0000000000b1', 1, 2, TRUE,
   '2026-01-03 00:00:00.000', '2026-01-03 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-0000000000b2'), 1, 1, 1, '',
   'albums/01900000-0000-7000-8000-0000000000b2', NULL, NULL, FALSE,
   '2026-01-03 00:00:00.000', '2026-01-06 00:00:00.000');

-- Sessions from before audience existed.
INSERT INTO sessions (id, public_id, user_id, refresh_hash, rotated_from_hash, expires_at, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000301'), 1, REPEAT('1', 64), REPEAT('0', 64),
   '2026-12-01 00:00:00.000', '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-000000000302'), 2, REPEAT('2', 64), NULL,
   '2026-12-01 00:00:00.000', '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

-- Allowed addresses: mixed case with a reason, plain, and withdrawn.
INSERT INTO oauth_allowed_emails (id, public_id, email, reason, created_by_user_id, enabled, created_at, updated_at) VALUES
  (1, UUID_TO_BIN('01900000-0000-7000-8000-000000000401'), 'Ops@Example.com', 'on-call contractor', 1, TRUE,
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2, UUID_TO_BIN('01900000-0000-7000-8000-000000000402'), 'guest@partner.example', '', NULL, TRUE,
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (3, UUID_TO_BIN('01900000-0000-7000-8000-000000000403'), 'Former@Partner.Example', 'left', 1, FALSE,
   '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000');

-- The event log as v0.1.0 wrote it: the old kind names, the subject's public
-- id in $.id (which the generated subject_public_id column read), the
-- feed line in $.summary, and a detail's own id in $.subject.
INSERT INTO events (id, public_id, workspace_id, calendar_id, actor_user_id, type, payload_json, occurred_at, created_at, updated_at) VALUES
  (1,  UUID_TO_BIN('01900000-0000-7000-8000-000000000601'), 1, 1, 1, 'calendar.created',
   '{"id":"01900000-0000-7000-8000-0000000000c1","summary":"Family"}', '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000', '2026-01-01 00:00:00.000'),
  (2,  UUID_TO_BIN('01900000-0000-7000-8000-000000000602'), 1, 1, 1, 'calendar.updated',
   '{"id":"01900000-0000-7000-8000-0000000000c1","summary":"Family"}', '2026-01-01 00:00:01.000', '2026-01-01 00:00:01.000', '2026-01-01 00:00:01.000'),
  (3,  UUID_TO_BIN('01900000-0000-7000-8000-000000000603'), 1, 1, 1, 'calendar.event.created',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Standup"}', '2026-01-01 00:00:02.000', '2026-01-01 00:00:02.000', '2026-01-01 00:00:02.000'),
  (4,  UUID_TO_BIN('01900000-0000-7000-8000-000000000604'), 1, 1, 1, 'calendar.event.updated',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Standup","scope":"occurrence","originalStart":"2026-01-20T09:00:00Z"}', '2026-01-01 00:00:03.000', '2026-01-01 00:00:03.000', '2026-01-01 00:00:03.000'),
  (5,  UUID_TO_BIN('01900000-0000-7000-8000-000000000605'), 1, 1, 1, 'calendar.event.deleted',
   '{"id":"01900000-0000-7000-8000-0000000000e5","summary":"Old plan"}', '2026-01-02 00:00:00.000', '2026-01-02 00:00:00.000', '2026-01-02 00:00:00.000'),
  (6,  UUID_TO_BIN('01900000-0000-7000-8000-000000000606'), 1, 1, 2, 'calendar.comment.added',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"see you there","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001c1"}', '2026-01-02 00:00:01.000', '2026-01-02 00:00:01.000', '2026-01-02 00:00:01.000'),
  (7,  UUID_TO_BIN('01900000-0000-7000-8000-000000000607'), 1, 1, 2, 'calendar.comment.edited',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"see you there!","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001c1"}', '2026-01-02 00:00:02.000', '2026-01-02 00:00:02.000', '2026-01-02 00:00:02.000'),
  (8,  UUID_TO_BIN('01900000-0000-7000-8000-000000000608'), 1, 1, 2, 'calendar.comment.removed',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Standup","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001c1"}', '2026-01-02 00:00:03.000', '2026-01-02 00:00:03.000', '2026-01-02 00:00:03.000'),
  (9,  UUID_TO_BIN('01900000-0000-7000-8000-000000000609'), 1, 1, 1, 'calendar.checklist.added',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Bring snacks","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001d1"}', '2026-01-02 00:00:04.000', '2026-01-02 00:00:04.000', '2026-01-02 00:00:04.000'),
  (10, UUID_TO_BIN('01900000-0000-7000-8000-000000000610'), 1, 1, 1, 'calendar.checklist.updated',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Bring fruit","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001d1"}', '2026-01-02 00:00:05.000', '2026-01-02 00:00:05.000', '2026-01-02 00:00:05.000'),
  (11, UUID_TO_BIN('01900000-0000-7000-8000-000000000611'), 1, 1, 1, 'calendar.checklist.removed',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Bring fruit","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001d1"}', '2026-01-02 00:00:06.000', '2026-01-02 00:00:06.000', '2026-01-02 00:00:06.000'),
  (12, UUID_TO_BIN('01900000-0000-7000-8000-000000000612'), 1, 1, 1, 'calendar.attachment.added',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"agenda.pdf","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001a2"}', '2026-01-04 00:00:01.000', '2026-01-04 00:00:01.000', '2026-01-04 00:00:01.000'),
  (13, UUID_TO_BIN('01900000-0000-7000-8000-000000000613'), 1, 1, 1, 'calendar.attachment.removed',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"agenda.pdf","event":"01900000-0000-7000-8000-0000000000e1","subject":"01900000-0000-7000-8000-0000000001a2"}', '2026-01-05 00:00:00.000', '2026-01-05 00:00:00.000', '2026-01-05 00:00:00.000'),
  (14, UUID_TO_BIN('01900000-0000-7000-8000-000000000614'), 1, 1, 2, 'calendar.attendee.updated',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Standup","event":"01900000-0000-7000-8000-0000000000e1","user":"01900000-0000-7000-8000-0000000000a2","rsvp":"accepted"}', '2026-01-05 00:00:01.000', '2026-01-05 00:00:01.000', '2026-01-05 00:00:01.000'),
  (15, UUID_TO_BIN('01900000-0000-7000-8000-000000000615'), 1, 1, 1, 'calendar.attendee.updated',
   '{"id":"01900000-0000-7000-8000-0000000000e1","summary":"Standup","event":"01900000-0000-7000-8000-0000000000e1","user":"01900000-0000-7000-8000-0000000000a2","canEdit":true}', '2026-01-05 00:00:02.000', '2026-01-05 00:00:02.000', '2026-01-05 00:00:02.000'),
  (16, UUID_TO_BIN('01900000-0000-7000-8000-000000000616'), 1, 1, 2, 'calendar.member.joined',
   '{"id":"01900000-0000-7000-8000-0000000000a2","summary":"Member"}', '2026-01-01 00:01:00.000', '2026-01-01 00:01:00.000', '2026-01-01 00:01:00.000'),
  (17, UUID_TO_BIN('01900000-0000-7000-8000-000000000617'), 1, 1, 1, 'calendar.member.role_changed',
   '{"id":"01900000-0000-7000-8000-0000000000a2","summary":"Member -> editor"}', '2026-01-01 00:02:00.000', '2026-01-01 00:02:00.000', '2026-01-01 00:02:00.000'),
  (18, UUID_TO_BIN('01900000-0000-7000-8000-000000000618'), 1, 1, 2, 'calendar.member.updated',
   '{"id":"01900000-0000-7000-8000-0000000000a2","summary":"Member"}', '2026-01-01 00:03:00.000', '2026-01-01 00:03:00.000', '2026-01-01 00:03:00.000'),
  (19, UUID_TO_BIN('01900000-0000-7000-8000-000000000619'), 1, 1, 2, 'calendar.member.left',
   '{"id":"01900000-0000-7000-8000-0000000000a2","summary":"Member"}', '2026-01-06 00:00:00.000', '2026-01-06 00:00:00.000', '2026-01-06 00:00:00.000'),
  (20, UUID_TO_BIN('01900000-0000-7000-8000-000000000620'), 1, 1, 1, 'calendar.member.removed',
   '{"id":"01900000-0000-7000-8000-0000000000a2","summary":"Member"}', '2026-01-06 00:00:01.000', '2026-01-06 00:00:01.000', '2026-01-06 00:00:01.000'),
  (21, UUID_TO_BIN('01900000-0000-7000-8000-000000000621'), 1, 1, 1, 'calendar.memo.created',
   '{"id":"01900000-0000-7000-8000-0000000000d1","summary":"Buy milk"}', '2026-01-01 00:04:00.000', '2026-01-01 00:04:00.000', '2026-01-01 00:04:00.000'),
  (22, UUID_TO_BIN('01900000-0000-7000-8000-000000000622'), 1, 1, 1, 'calendar.invite.created',
   '{"id":"01900000-0000-7000-8000-0000000000f1","summary":"editor","public":false}', '2026-01-01 00:05:00.000', '2026-01-01 00:05:00.000', '2026-01-01 00:05:00.000'),
  (23, UUID_TO_BIN('01900000-0000-7000-8000-000000000623'), 1, 1, 1, 'calendar.invite.revoked',
   '{"id":"01900000-0000-7000-8000-0000000000f1"}', '2026-01-01 00:06:00.000', '2026-01-01 00:06:00.000', '2026-01-01 00:06:00.000'),
  (24, UUID_TO_BIN('01900000-0000-7000-8000-000000000624'), 1, 1, 1, 'calendar.photo.uploaded',
   '{"id":"01900000-0000-7000-8000-0000000000b1","summary":"Beach"}', '2026-01-03 00:00:01.000', '2026-01-03 00:00:01.000', '2026-01-03 00:00:01.000'),
  (25, UUID_TO_BIN('01900000-0000-7000-8000-000000000625'), 1, 1, 1, 'calendar.photo.deleted',
   '{"id":"01900000-0000-7000-8000-0000000000b2"}', '2026-01-06 00:00:02.000', '2026-01-06 00:00:02.000', '2026-01-06 00:00:02.000');
