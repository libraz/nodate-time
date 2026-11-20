import { describe, expect, it } from 'vitest';
import { isGroupEvent } from './event-group';

describe('isGroupEvent', () => {
  it('is false without participants', () => {
    expect(isGroupEvent({ participants: [] })).toBe(false);
    expect(isGroupEvent({} as { participants: string[] })).toBe(false);
  });

  it('is false for a single participant', () => {
    expect(isGroupEvent({ participants: ['a'] })).toBe(false);
  });

  it('is true for two or more participants', () => {
    expect(isGroupEvent({ participants: ['a', 'b'] })).toBe(true);
    expect(isGroupEvent({ participants: ['a', 'b', 'c'] })).toBe(true);
  });
});
