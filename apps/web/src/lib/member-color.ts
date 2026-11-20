/** Ink drawn on a member's colour: whichever of the two reads better on it. */
export const INK_LIGHT = '#FFFFFF';
export const INK_DARK = '#1D1D1F';

const HEX6 = /^#([0-9a-f]{6})$/i;
const HEX_INPUT = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i;
const RGB_INPUT = /^(?:rgb\s*\()?\s*(\d{1,3})\s*[,\s]\s*(\d{1,3})\s*[,\s]\s*(\d{1,3})\s*\)?$/i;

function channels(hex: string): [number, number, number] | null {
  const m = hex.match(HEX6);
  if (!m?.[1]) return null;
  const n = Number.parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

function luminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

const DARK_LUMINANCE = luminance([29, 29, 31]);

/**
 * The ink with the higher WCAG contrast on a fill. A member may pick any
 * colour, so white text on a yellow layer cannot be assumed to read.
 */
export function readableInk(fill: string): string {
  const rgb = channels(fill);
  if (!rgb) return INK_LIGHT;
  const l = luminance(rgb);
  const onLight = 1.05 / (l + 0.05);
  const onDark = (l + 0.05) / (DARK_LUMINANCE + 0.05);
  return onDark > onLight ? INK_DARK : INK_LIGHT;
}

/**
 * Reads what a person typed as a colour: `#RRGGBB`, `#RGB` (either without the
 * `#`), or `R, G, B` with or without `rgb(...)`. Returns `#RRGGBB` in upper
 * case, the shape the API stores, or null when it is not a colour.
 */
export function parseColorInput(input: string): string | null {
  const s = input.trim();
  const hex = s.match(HEX_INPUT);
  if (hex?.[1]) {
    const h = hex[1].length === 3 ? [...hex[1]].map((c) => c + c).join('') : hex[1];
    return `#${h.toUpperCase()}`;
  }
  const rgb = s.match(RGB_INPUT);
  if (rgb) {
    const parts = rgb.slice(1, 4).map(Number);
    if (parts.some((p) => p > 255)) return null;
    return `#${parts.map((p) => p.toString(16).padStart(2, '0')).join('')}`.toUpperCase();
  }
  return null;
}

/** `#RRGGBB` as its three channels, for showing a colour as RGB. */
export function rgbOf(hex: string): [number, number, number] | null {
  return channels(hex);
}
