import { describe, expect, it } from 'vitest';
import { activityColor, activityLabelKey, activityVerb } from './activity';

// These are the strings the server actually sends. A UI vocabulary that does
// not include them leaves every row in the feed unlabelled and uncoloured,
// which is what a fixture written in the client's own terms will never catch.
const SERVER_ACTIONS = [
  'calendar.created',
  'calendar.updated',
  'calendar.deleted',
  'calendar.subscribed',
  'calendar.subscription.updated',
  'calendar.member.added',
  'calendar.member.removed',
  'calendar.member.role_changed',
  'calendar.member.updated',
  'calendar.event.created',
  'calendar.event.updated',
  'calendar.event.deleted',
  'calendar.event.comment.created',
  'calendar.event.comment.updated',
  'calendar.event.comment.deleted',
  'calendar.event.checklist.created',
  'calendar.event.checklist.updated',
  'calendar.event.checklist.deleted',
  'calendar.event.attachment.created',
  'calendar.event.attachment.deleted',
  'calendar.event.attendee.added',
  'calendar.event.attendee.removed',
  'calendar.event.attendee.updated',
  'calendar.event.rsvp.updated',
  'calendar.event.invite.created',
  'calendar.event.invite.rotated',
  'calendar.event.invite.revoked',
  'calendar.memo.created',
  'calendar.memo.updated',
  'calendar.memo.completed',
  'calendar.memo.deleted',
  'calendar.reminder',
  'calendar.invite.created',
  'calendar.invite.revoked',
  'calendar.photo.uploaded',
  'calendar.photo.updated',
  'calendar.photo.deleted',
  'workspace.member.added',
  'workspace.member.removed',
  'workspace.member.role_changed',
];

describe('activityVerb', () => {
  it('recognises every action the server records', () => {
    for (const action of SERVER_ACTIONS) {
      expect(activityVerb(action), action).not.toBe('unknown');
    }
  });

  it('reads the verb from the last segment, so a new entity needs no change', () => {
    expect(activityVerb('calendar.reminder.created')).toBe('created');
    expect(activityVerb('calendar.event.deleted')).toBe('deleted');
  });

  it('folds the arrival and departure of a thing onto one pair of verbs', () => {
    expect(activityVerb('calendar.photo.uploaded')).toBe('created');
    expect(activityVerb('calendar.event.comment.created')).toBe('created');
    expect(activityVerb('calendar.event.attachment.deleted')).toBe('deleted');
    expect(activityVerb('calendar.event.comment.updated')).toBe('updated');
    expect(activityVerb('calendar.event.attendee.added')).toBe('created');
    expect(activityVerb('calendar.subscribed')).toBe('created');
    expect(activityVerb('calendar.event.invite.rotated')).toBe('updated');
  });

  it('gives a completed memo and a reminder their own labels', () => {
    expect(activityLabelKey('calendar.memo.completed')).toBe('activity.completed');
    expect(activityLabelKey('calendar.reminder')).toBe('activity.reminded');
  });

  it('labels no registered kind as an unknown change', () => {
    for (const action of SERVER_ACTIONS) {
      expect(activityLabelKey(action), action).not.toBe('activity.changed');
    }
  });

  it('still labels something it does not recognise', () => {
    expect(activityLabelKey('calendar.event.frobnicated')).toBe('activity.changed');
  });
});

describe('activityColor', () => {
  it('draws arrivals and losses differently', () => {
    expect(activityColor('calendar.event.created')).toBe('var(--color-accent)');
    expect(activityColor('calendar.event.deleted')).toBe('var(--color-danger)');
    expect(activityColor('calendar.event.updated')).toBe('var(--color-text-tertiary)');
  });

  it('never leaves a colour undefined', () => {
    for (const action of SERVER_ACTIONS) {
      expect(activityColor(action), action).toMatch(/^var\(--/);
    }
  });
});
