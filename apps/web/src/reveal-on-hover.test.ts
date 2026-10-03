import { describe, expect, it } from 'vitest';

// Read from disk: the test runner hands stylesheets over empty (`css: false`).
// The module name is a variable so the DOM-only type environment is not asked
// to resolve it.
const fsModule = 'node:fs';
const { readFileSync } = (await import(/* @vite-ignore */ fsModule)) as {
  readFileSync: (path: string, encoding: 'utf8') => string;
};
const css = readFileSync('src/index.css', 'utf8');

const sources = import.meta.glob(['./**/*.tsx', '!./**/*.test.tsx'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

describe('hover-revealed controls', () => {
  // A touch screen never hovers, so a control hidden until hover is a control
  // a phone user cannot reach.
  it('hides them only where the pointer can hover', () => {
    const hoverBlock = css.match(/@media \(hover: hover\) and \(pointer: fine\) \{[\s\S]*?\n\}/);
    expect(hoverBlock?.[0]).toMatch(/\.reveal-on-hover:not\(:focus-within\)\s*\{\s*opacity: 0;/);
    const outside = css.replace(hoverBlock?.[0] ?? '', '');
    expect(outside).not.toMatch(/\.reveal-on-hover[^{]*\{[^}]*opacity:\s*0/);
  });

  it('are all built on the shared class', () => {
    const offenders = Object.entries(sources)
      .filter(([, src]) => /opacity-0 group-hover:opacity-100/.test(src))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });
});
