-- ====================================
-- events.subject_public_id — product-layer index for one entity's history
--
-- The event modal opens one event's own history, which means every log row
-- whose payload names that event. The contract gives the payload no fixed
-- schema, so the subject can only be matched inside the JSON -- and a JSON
-- predicate is not indexable, so the match read every row the calendar had
-- ever produced. LIMIT does not help: the limit applies after the scan.
--
-- The effect is backwards. Opening an event is the single most frequent
-- thing anyone does here, and it got slower in proportion to how much the
-- calendar had been used, which is to say it punished the calendars that
-- were working.
--
-- A stored generated column gives the same value a name the optimiser can
-- reach. It lives here rather than in core/ because the payload key it reads
-- is this product's convention: the contract promises a JSON document, not
-- that `$.id` identifies a subject.
-- ====================================
ALTER TABLE events
  ADD COLUMN subject_public_id VARCHAR(36)
    CHARACTER SET latin1 COLLATE latin1_swedish_ci
    GENERATED ALWAYS AS (payload_json->>'$.id') STORED
    COMMENT 'Public id of the entity this event is about, lifted out of the payload so one entity history can be indexed. Generated: never written directly.',
  ADD KEY idx_events_workspace_calendar_subject
    (workspace_id, calendar_id, subject_public_id, id);
