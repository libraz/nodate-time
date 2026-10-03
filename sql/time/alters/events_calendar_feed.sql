-- ====================================
-- events (workspace_id, calendar_id, id) — product-layer index for the feed
--
-- The activity feed pages one calendar's log by id, newest first. The
-- contract's calendar index ends in occurred_at, so the planner either
-- walked the primary key backwards filtering every other calendar's rows out
-- (the first page) or read the calendar's whole history and sorted it (a
-- page after a cursor). Either way a page cost followed how much had ever
-- been written, and LIMIT applies after that work, not to it.
--
-- With id as the last column the same predicate is a range over exactly this
-- calendar's rows in the order the query wants, and a page stops at LIMIT.
--
-- It lives here rather than in core/ because paging by id is this product's
-- choice; the contract only promises an append-only log.
-- ====================================
ALTER TABLE events
  ADD KEY idx_events_workspace_calendar_id (workspace_id, calendar_id, id);
