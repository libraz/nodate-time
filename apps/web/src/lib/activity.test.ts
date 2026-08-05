import { describe, expect, it } from 'vitest';
import { activityColor, activityEntity, activityLabelKey, activityVerb } from './activity';

// These are the strings the server actually sends. A UI vocabulary that does
// not include them leaves every row in the feed unlabelled and uncoloured,
// which is what a fixture written in the client's own terms will never catch.
const SERVER_ACTIONS = [
  'calendar.event.created',
  'calendar.event.updated',
  'calendar.event.deleted',
  'calendar.memo.created',
  'calendar.memo.updated',
  'calendar.memo.deleted',
  'calendar.member.joined',
  'calendar.member.left',
  'calendar.member.removed',
  'calendar.member.role_changed',
  'calendar.invite.created',
  'calendar.invite.revoked',
  'calendar.photo.uploaded',
  'calendar.photo.updated',
  'calendar.photo.deleted',
  'calendar.comment.added',
  'calendar.comment.edited',
  'calendar.comment.removed',
  'calendar.checklist.added',
  'calendar.checklist.updated',
  'calendar.checklist.removed',
  'calendar.attachment.added',
  'calendar.attachment.removed',
  'calendar.created',
  'calendar.updated',
  'calendar.deleted',
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
    expect(activityVerb('calendar.comment.added')).toBe('created');
    expect(activityVerb('calendar.comment.removed')).toBe('deleted');
    expect(activityVerb('calendar.comment.edited')).toBe('updated');
  });

  it('still labels something it does not recognise', () => {
    expect(activityLabelKey('calendar.event.frobnicated')).toBe('activity.changed');
  });
});

describe('activityEntity', () => {
  it('names the entity a dotted action is about', () => {
    expect(activityEntity('calendar.event.created')).toBe('event');
    expect(activityEntity('calendar.member.role_changed')).toBe('member');
  });

  it('treats a two-segment action as being about the calendar itself', () => {
    expect(activityEntity('calendar.created')).toBe('calendar');
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
