import { describe, expect, it } from 'vitest';
import { isGroupEvent } from './event-group';

describe('isGroupEvent', () => {
  it('is false without participantIds', () => {
    expect(isGroupEvent({ participantIds: [] })).toBe(false);
    expect(isGroupEvent({} as { participantIds: string[] })).toBe(false);
  });

  it('is false for a single participant', () => {
    expect(isGroupEvent({ participantIds: ['a'] })).toBe(false);
  });

  it('is true for two or more participantIds', () => {
    expect(isGroupEvent({ participantIds: ['a', 'b'] })).toBe(true);
    expect(isGroupEvent({ participantIds: ['a', 'b', 'c'] })).toBe(true);
  });
});
