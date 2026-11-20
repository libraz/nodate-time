import { describe, expect, it } from 'vitest';
import { MEMBER_COLORS } from '@/types/calendar';
import { INK_DARK, INK_LIGHT, parseColorInput, readableInk, rgbOf } from './member-color';

describe('readableInk', () => {
  it('puts dark ink on light fills and light ink on dark ones', () => {
    expect(readableInk('#FFFFFF')).toBe(INK_DARK);
    expect(readableInk('#FDC02D')).toBe(INK_DARK);
    expect(readableInk('#000000')).toBe(INK_LIGHT);
    expect(readableInk('#E73B3B')).toBe(INK_LIGHT);
  });

  it('chooses an ink for every palette colour', () => {
    for (const c of MEMBER_COLORS) expect([INK_DARK, INK_LIGHT]).toContain(readableInk(c));
  });

  it('falls back to light ink on something that is not a colour', () => {
    expect(readableInk('blue')).toBe(INK_LIGHT);
  });
});

describe('parseColorInput', () => {
  it.each([
    ['#47b2f7', '#47B2F7'],
    ['47B2F7', '#47B2F7'],
    ['#abc', '#AABBCC'],
    ['  fff ', '#FFFFFF'],
    ['rgb(71, 178, 247)', '#47B2F7'],
    ['71,178,247', '#47B2F7'],
    ['71 178 247', '#47B2F7'],
  ])('reads %s as %s', (input, want) => {
    expect(parseColorInput(input)).toBe(want);
  });

  it.each(['', '#12345', 'red', 'rgb(256, 0, 0)', '1,2', '#GGGGGG'])('refuses %s', (input) => {
    expect(parseColorInput(input)).toBeNull();
  });
});

describe('rgbOf', () => {
  it('splits a colour into channels', () => {
    expect(rgbOf('#47B2F7')).toEqual([71, 178, 247]);
    expect(rgbOf('nope')).toBeNull();
  });
});
