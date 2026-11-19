ALTER TABLE calendar_schedule_polls
  ADD CONSTRAINT fk_calendar_schedule_polls_workspace FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE CASCADE ON UPDATE RESTRICT;
ALTER TABLE calendar_schedule_polls
  ADD CONSTRAINT fk_calendar_schedule_polls_calendar FOREIGN KEY (calendar_id) REFERENCES calendars(id) ON DELETE CASCADE ON UPDATE RESTRICT;
ALTER TABLE calendar_schedule_polls
  ADD CONSTRAINT fk_calendar_schedule_polls_creator FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL ON UPDATE RESTRICT;
ALTER TABLE calendar_schedule_polls
  ADD CONSTRAINT fk_calendar_schedule_polls_manager FOREIGN KEY (manager_user_id) REFERENCES users(id) ON DELETE SET NULL ON UPDATE RESTRICT;

ALTER TABLE calendar_schedule_poll_slots
  ADD CONSTRAINT fk_calendar_schedule_poll_slots_poll FOREIGN KEY (poll_id) REFERENCES calendar_schedule_polls(id) ON DELETE CASCADE ON UPDATE RESTRICT;

ALTER TABLE calendar_schedule_poll_members
  ADD CONSTRAINT fk_calendar_schedule_poll_members_poll FOREIGN KEY (poll_id) REFERENCES calendar_schedule_polls(id) ON DELETE CASCADE ON UPDATE RESTRICT;
ALTER TABLE calendar_schedule_poll_members
  ADD CONSTRAINT fk_calendar_schedule_poll_members_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE ON UPDATE RESTRICT;

ALTER TABLE calendar_schedule_poll_answers
  ADD CONSTRAINT fk_calendar_schedule_poll_answers_slot FOREIGN KEY (poll_id, slot_id) REFERENCES calendar_schedule_poll_slots(poll_id, id) ON DELETE CASCADE ON UPDATE RESTRICT,
  ADD CONSTRAINT fk_calendar_schedule_poll_answers_member FOREIGN KEY (poll_id, member_id, user_id) REFERENCES calendar_schedule_poll_members(poll_id, id, user_id) ON DELETE CASCADE ON UPDATE RESTRICT;
