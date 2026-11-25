import { describe, expect, it } from 'vitest';
import { FALLBACK_LABEL_COLOR, MEMBER_COLORS } from './calendar';

// The server's member palette (memberpalette), pinned on both sides. A palette
// that drifts hands out a colour the server's list does not contain, which
// shows up as a swatch nobody can select again.
const SERVER_PALETTE = [
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
  it("matches the server's member palette", () => {
    expect([...MEMBER_COLORS]).toEqual(SERVER_PALETTE);
  });

  it('falls back to a colour that is in the palette', () => {
    expect(SERVER_PALETTE).toContain(FALLBACK_LABEL_COLOR);
  });
});
