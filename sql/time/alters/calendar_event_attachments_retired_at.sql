-- ====================================
-- calendar_event_attachments.retired_at — tell a deleted attachment from one
-- that was never confirmed
--
-- enabled = FALSE is two states in the contract: a reservation whose upload
-- has not been confirmed, and a confirmed attachment that was deleted. They
-- end differently. A reservation may still be confirmed, and is dropped by the
-- abandoned-upload sweep once its URL is long dead; a deleted attachment must
-- never come back, and is kept for the activity history until its retention
-- runs out. Read off enabled alone, a deleted row confirms back to life and is
-- collected on the reservation's clock.
--
-- Set by every soft delete, in the same statement that clears enabled.
-- ====================================
ALTER TABLE calendar_event_attachments
  ADD COLUMN retired_at DATETIME(3) NULL
    COMMENT 'When a confirmed attachment was deleted. NULL on a live row and on a reservation that was never confirmed.'
    AFTER enabled;
