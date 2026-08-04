-- ====================================
-- events
-- Append-only event log. All workspace state transitions flow through this
-- table, and every other process on the database reads it rather than
-- polling: it is the only channel two independently deployed products
-- share. The API offers no DELETE endpoint; purgeWorkspace is the sole
-- deletion path (test fixtures only).
--
-- Consumers tail the log with `WHERE id > last_seen`. AUTO_INCREMENT
-- assigns an id when the INSERT runs but the row stays invisible until
-- its transaction commits, so a row can appear below an id that has
-- already been read. A tailer that advances straight to the highest id
-- it has seen drops those rows permanently; it must re-read from below
-- its high-water mark until a row has had time to commit.
-- ====================================
CREATE TABLE events (
  -- BIGINT UNSIGNED is a deliberate exception to the project default
  -- (INT UNSIGNED for IDs). Justification: this is an append-only event log
  -- expected to grow indefinitely; the ~4.29B INT UNSIGNED ceiling is
  -- reachable in long-lived deployments. Any FK a product layer points at
  -- this column must match the type.
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY COMMENT 'Internal PK, never exposed; BIGINT UNSIGNED for unbounded append-only growth',
  public_id BINARY(16) NOT NULL COMMENT 'UUID v7, the only externally visible ID',
  workspace_id INT UNSIGNED NOT NULL COMMENT 'Internal FK to workspaces.id',
  task_id INT UNSIGNED NULL COMMENT 'Internal FK to tasks.id when the event targets a task',
  triggered_by_signal_id INT UNSIGNED NULL COMMENT 'Internal FK to signals.id; set when this event was emitted by the Applier in response to a judged signal. Provides full traceability from external input to task event. Belongs to a product layer: NULL in a deployment without one.',
  actor_user_id INT UNSIGNED NULL COMMENT 'Acting user.id (null for system/bot actions). Mutually exclusive with actor_agent_id and actor_system_source: exactly one of the three actor sources is set per row (both NULL is also legal for legacy "system actor"). The mutual-exclusion rule is enforced by query design and handler validation, not a CHECK constraint, because all three FK referential actions use ON DELETE SET NULL and MySQL 8.4 forbids CHECK constraints referencing columns used in FK referential actions. Every INSERT must therefore bind exactly one of the three, chosen by who is acting: a person, an agent, or a background process.',
  actor_agent_id INT UNSIGNED NULL COMMENT 'Acting ai_agents.id when the event was produced by an AI agent (judge / task agent). See actor_user_id comment for the three-way exclusion rule.',
  actor_system_source VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NULL COMMENT 'Third actor source, for events emitted by a background process rather than a person or an agent. Free-form and namespaced by the writer, e.g. `worker:scheduler` or `worker:retention`. Not an FK because such a process has no row in the database. See actor_user_id comment for the three-way exclusion rule.',
  reverses_event_id BIGINT UNSIGNED NULL COMMENT 'Internal FK to events.id. Non-NULL means this event is a compensating reverse of another event (e.g., user undoing an auto-completion). A projection reading the log cancels both events out. The log is immutable: a reversal is a new row, never an UPDATE or DELETE of the original.',

  type VARCHAR(64) CHARACTER SET latin1 COLLATE latin1_swedish_ci NOT NULL COMMENT 'Event type (e.g., task.created, signal.attached, signal.judged)',
  payload_json JSON NOT NULL CHECK (JSON_VALID(payload_json)) COMMENT 'Event payload',
  occurred_at DATETIME(3) NOT NULL COMMENT 'Logical time of the event (millisecond precision; ties broken by id)',

  sort_weight INT NOT NULL DEFAULT 0 COMMENT 'Display order',
  notes TEXT NULL COMMENT 'Admin notes',
  enabled BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enabled flag',
  updated_at TIMESTAMP(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

  UNIQUE KEY uniq_events_public_id (public_id),
  UNIQUE KEY uniq_events_workspace_public_id (workspace_id, public_id),
  KEY idx_events_workspace_id_occurred_at (workspace_id, occurred_at),
  KEY idx_events_workspace_id_task_id_occurred_at (workspace_id, task_id, occurred_at),
  KEY idx_events_workspace_id_type (workspace_id, type),
  KEY idx_events_workspace_id_actor_agent_id (workspace_id, actor_agent_id),
  KEY idx_events_triggered_by_signal (triggered_by_signal_id),
  -- UNIQUE guards against duplicate compensating reversals: two concurrent
  -- reverses of the same event would otherwise both insert a compensating
  -- row and double-cancel in any projection over the log. MySQL treats
  -- multiple NULLs as distinct, so ordinary (non-reverse) events with
  -- reverses_event_id IS NULL are unaffected; only genuine reverses dedupe.
  UNIQUE KEY uniq_events_reverses (workspace_id, reverses_event_id),

  -- fk_events_task, fk_events_actor_agent and fk_events_triggered_by_signal
  -- are NOT declared here. Their columns are core, so every writer produces
  -- rows of the same shape, but `tasks`, `ai_agents` and `signals` belong to
  -- a product layer and a foreign key cannot point at a table that may not
  -- exist. The layer owning those tables adds the three constraints from
  -- its own constraints/ directory; elsewhere the columns are always NULL.
  CONSTRAINT fk_events_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE,
  CONSTRAINT fk_events_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL,
  CONSTRAINT fk_events_reverses FOREIGN KEY (reverses_event_id) REFERENCES events(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Append-only event log';
