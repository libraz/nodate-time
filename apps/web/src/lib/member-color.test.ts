import { describe, expect, it } from 'vitest';
import { COLOR_SWATCHES, MEMBER_COLORS } from '@/types/calendar';
import {
  hexToHsv,
  hsvToHex,
  INK_DARK,
  INK_LIGHT,
  parseColorInput,
  readableInk,
  rgbOf,
} from './member-color';

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

describe('hexToHsv / hsvToHex', () => {
  it('reads the primaries and greys on their axes', () => {
    expect(hexToHsv('#FF0000')).toEqual({ h: 0, s: 1, v: 1 });
    expect(hexToHsv('#00FF00')).toEqual({ h: 120, s: 1, v: 1 });
    expect(hexToHsv('#0000FF')).toEqual({ h: 240, s: 1, v: 1 });
    expect(hexToHsv('#808080')).toMatchObject({ h: 0, s: 0 });
    expect(hexToHsv('#000000')).toEqual({ h: 0, s: 0, v: 0 });
  });

  it('round-trips every swatch exactly', () => {
    for (const c of COLOR_SWATCHES) {
      const hsv = hexToHsv(c);
      expect(hsv).not.toBeNull();
      expect(hsvToHex(hsv as NonNullable<typeof hsv>)).toBe(c);
    }
  });

  it('writes upper-case #RRGGBB', () => {
    expect(hsvToHex({ h: 30, s: 0.5, v: 0.8 })).toMatch(/^#[0-9A-F]{6}$/);
  });

  it('refuses something that is not a colour', () => {
    expect(hexToHsv('blue')).toBeNull();
  });
});
