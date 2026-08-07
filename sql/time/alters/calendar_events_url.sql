-- ====================================
-- calendar_events.url — hold an internationalised address
--
-- The column is the one place in the contract where a value a person types,
-- or an imported file supplies, was given a latin1 charset. Every other
-- column carrying free user text is utf8mb4, including the five sibling URL
-- columns of the same VARCHAR(2048) shape on users, calendars, workspaces
-- and calendar_public_shares. An address is not an identifier the way a hex
-- hash or an IANA zone name is: RFC 3987 lets one carry non-ASCII directly,
-- and a browser hands it back to be pasted in exactly that form.
--
-- Under latin1 MySQL refuses the whole value -- `Incorrect string value` --
-- rather than storing what it can. On the import path that refusal is not
-- confined to the link. Each event is written in its own transaction, so the
-- insert rolls back and the event itself is gone, counted as failed. A file
-- with nothing wrong with it arrives short, and what it is short by is an
-- optional property nobody thinks to check.
--
-- Nothing indexes url, so widening it costs no key: the table's keys cover
-- public_id, the calendar and workspace ranges, the task projection and the
-- recurrence override, and the fulltext index is over title and memo. The
-- declared row stays far inside the 65,535-byte limit.
--
-- It lives here rather than in core/ because the contract has already said
-- what the column is. Which alphabet its content may use is a decision this
-- product is making about its own users, and keeping it as an ALTER leaves
-- the vendored contract byte-identical to upstream.
--
-- sqlc reads a MODIFY as a drop and re-add, so url moves to the end of the
-- generated struct and of every expanded SELECT. Position is not semantics
-- here -- the generated queries name their columns and scan in the order
-- they named them -- but the diff is larger than the change, and an AFTER
-- clause does not talk sqlc out of it.
-- ====================================
ALTER TABLE calendar_events
  MODIFY COLUMN url VARCHAR(2048)
    CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci
    NULL COMMENT 'Meeting link or related URL. utf8mb4 rather than latin1: an internationalised address (RFC 3987) carries non-ASCII directly.';
