import { describe, expect, it } from 'vitest';
import { FALLBACK_LABEL_COLOR, MEMBER_COLORS, normalizeNotificationOffset } from './calendar';

// The same list the API serves at /calendars/{id}/labels, pinned on both
// sides. A palette that drifts hands out a colour the calendar's own list does
// not contain, which shows up as a swatch nobody can select again.
const SERVED_PALETTE = [
  '#47B2F7',
  '#F35F8C',
  '#B38BDC',
  '#FDC02D',
  '#E73B3B',
  '#2ECC87',
  '#F5A623',
  '#8F8F8F',
  '#42A5F5',
  '#FF7043',
];

describe('member palette', () => {
  it('matches the one the API serves', () => {
    expect([...MEMBER_COLORS]).toEqual(SERVED_PALETTE);
  });

  it('falls back to a colour that is in the palette', () => {
    expect(SERVED_PALETTE).toContain(FALLBACK_LABEL_COLOR);
  });
});

describe('normalizeNotificationOffset', () => {
  it('keeps an offset the API accepts', () => {
    expect(normalizeNotificationOffset(30)).toBe(30);
    expect(normalizeNotificationOffset(0)).toBe(0);
  });

  // An offset the API rejects would fail the whole save, so it is dropped
  // rather than sent.
  it('drops one it does not', () => {
    expect(normalizeNotificationOffset(7)).toBeNull();
    expect(normalizeNotificationOffset(null)).toBeNull();
    expect(normalizeNotificationOffset(undefined)).toBeNull();
  });
});
