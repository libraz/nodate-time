import { describe, expect, it } from 'vitest';
import { flexibilityLabelKey, showAsClass } from './event-style';

describe('showAsClass', () => {
  it('leaves busy unmodified and names the other states', () => {
    expect(showAsClass('busy')).toBe('');
    expect(showAsClass('tentative')).toBe('evt-tentative');
    expect(showAsClass('free')).toBe('evt-free');
    expect(showAsClass('oof')).toBe('evt-oof');
  });
});

describe('flexibilityLabelKey', () => {
  it('has no label for fixed', () => {
    expect(flexibilityLabelKey('fixed', { short: false })).toBeNull();
    expect(flexibilityLabelKey('fixed', { short: true })).toBeNull();
  });

  it('uses the long keys by default and the short keys on request', () => {
    expect(flexibilityLabelKey('negotiable', { short: false })).toBe('event.flexibilityNegotiable');
    expect(flexibilityLabelKey('conditional', { short: false })).toBe(
      'event.flexibilityConditional',
    );
    expect(flexibilityLabelKey('negotiable', { short: true })).toBe(
      'event.flexibilityShort.negotiable',
    );
    expect(flexibilityLabelKey('conditional', { short: true })).toBe(
      'event.flexibilityShort.conditional',
    );
    expect(flexibilityLabelKey('negotiable')).toBe('event.flexibilityNegotiable');
  });
});
