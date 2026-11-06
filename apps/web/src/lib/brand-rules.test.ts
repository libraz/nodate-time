import { describe, expect, it } from 'vitest';
import {
  type Brand,
  DEFAULT_APP_NAME,
  faviconTags,
  imageOrigins,
  resolveBrand,
} from './brand-rules';

// biome-ignore lint/style/useNamingConvention: the build variable's name
const name = (value: unknown) => resolveBrand({ VITE_APP_NAME: value }).appName;
describe('resolveBrand name', () => {
  it('defaults when unset, empty or blank', () => {
    expect(name(undefined)).toBe(DEFAULT_APP_NAME);
    expect(name('')).toBe(DEFAULT_APP_NAME);
    expect(name('   ')).toBe(DEFAULT_APP_NAME);
  });

  it('trims surrounding whitespace', () => {
    expect(name('  Acme Cal  ')).toBe('Acme Cal');
  });

  it('counts code points, not UTF-16 units', () => {
    const astral64 = '\u{1F600}'.repeat(64);
    expect(name(astral64)).toBe(astral64);
    expect(() => name('\u{1F600}'.repeat(65))).toThrow('VITE_APP_NAME');
    expect(name('a'.repeat(64))).toBe('a'.repeat(64));
    expect(() => name('a'.repeat(65))).toThrow('VITE_APP_NAME');
  });

  it.each([
    ['LF', 'a\nb'],
    ['CR', 'a\rb'],
    ['NUL', 'a\u0000b'],
    ['DEL', 'a\u007Fb'],
    ['C1 start', 'a\u0080b'],
    ['C1 end', 'a\u009Fb'],
    ['line separator', 'a\u2028b'],
    ['paragraph separator', 'a\u2029b'],
    ['bidi embedding', 'a\u202Ab'],
    ['bidi override', 'a\u202Eb'],
    ['bidi isolate', 'a\u2066b'],
    ['bidi isolate end', 'a\u2069b'],
    ['colon', 'a:b'],
  ])('rejects %s', (_label, value) => {
    expect(() => name(value)).toThrow('VITE_APP_NAME');
  });

  it('keeps the offending value out of the message', () => {
    expect(() => name('secret:value')).not.toThrow(/secret/);
  });
});

const L = 'VITE_APP_LOGO_URL';
const LD = 'VITE_APP_LOGO_DARK_URL';
const I = 'VITE_APP_ICON_URL';
const ID = 'VITE_APP_ICON_DARK_URL';
const HTTPS_ERR = (v: string) =>
  `${v} must be an https URL or a data:image/(png|svg+xml|webp|jpeg|gif) URI`;
const ASCII_ERR = (v: string) => `${v} must contain only printable ASCII characters`;
const PNG = 'data:image/png;base64,';

const logo = (value: unknown) => resolveBrand({ [L]: value }).logo?.src;
const icon = (value: unknown) => resolveBrand({ [I]: value }).icon?.src;

describe.each([
  ['logo', L, logo],
  ['icon', I, icon],
] as const)('resolveBrand %s', (_kind, v, resolve) => {
  it.each(['', '   ', '\t\n'])('is unset for %j', (value) => {
    expect(resolve(value)).toBeUndefined();
  });

  it('is unset when the variable is absent', () => {
    expect(resolve(undefined)).toBeUndefined();
  });

  it('trims only surrounding ASCII whitespace', () => {
    expect(resolve(' https://cdn.example/l.svg ')).toBe('https://cdn.example/l.svg');
  });

  it.each([
    'HTTPS://cdn.example/l.svg',
    'DATA:image/png;base64,AAAA',
    'data:image/svg+xml,%3Csvg%3E',
  ])('accepts %s unchanged', (value) => {
    expect(resolve(value)).toBe(value);
  });

  it.each([
    'data:image/bmp;base64,AAAA',
    'data:text/html,hi',
    'http://cdn.example/l.svg',
    'https://',
    'cdn.example/l.svg',
    'javascript:alert(1)',
  ])('rejects %s as neither https nor an image data URI', (value) => {
    expect(() => resolve(value)).toThrow(HTTPS_ERR(v));
  });

  it.each(['https://cdn.example/a b.svg', 'https://a.example/x\ny', 'https://例え.jp/l.svg'])(
    'rejects %j as not printable ASCII',
    (value) => {
      expect(() => resolve(value)).toThrow(ASCII_ERR(v));
    },
  );

  it('enforces the data URI length limit', () => {
    expect(resolve(PNG + 'A'.repeat(32_768 - PNG.length))).toHaveLength(32_768);
    expect(() => resolve(PNG + 'A'.repeat(32_769 - PNG.length))).toThrow(
      `${v} data URI must be at most 32768 characters`,
    );
  });
});

describe.each([
  ['logo', L, LD, 'logo'],
  ['icon', I, ID, 'icon'],
] as const)('resolveBrand %s dark variant', (_kind, base, dark, key) => {
  const dk = 'https://cdn.example/d.svg';

  it.each([undefined, '', '   '])('requires the base when the base is %j', (b) => {
    expect(() => resolveBrand({ [base]: b, [dark]: dk })).toThrow(`${dark} requires ${base}`);
  });

  it('treats a blank dark value as unset', () => {
    expect(resolveBrand({ [base]: 'https://cdn.example/l.svg', [dark]: '   ' })[key]).toEqual({
      src: 'https://cdn.example/l.svg',
      darkSrc: undefined,
    });
  });

  it('carries the dark value through', () => {
    expect(resolveBrand({ [base]: 'https://cdn.example/l.svg', [dark]: dk })[key]?.darkSrc).toBe(
      dk,
    );
  });

  it('validates the dark value by the same rules', () => {
    expect(() =>
      resolveBrand({ [base]: 'https://cdn.example/l.svg', [dark]: 'http://x.example/' }),
    ).toThrow(HTTPS_ERR(dark));
  });
});

const brandOf = (env: Record<string, unknown>): Brand => resolveBrand(env);

describe('imageOrigins', () => {
  it('lists each https origin once, space-separated', () => {
    const b = brandOf({
      [L]: 'https://cdn.example.com/a/logo.svg?v=1',
      [LD]: 'https://cdn.example.com/a/logo-dark.svg',
      [I]: 'https://img.example.net/icon.svg',
      [ID]: 'https://cdn.example.com/icon-dark.svg',
    });
    expect(imageOrigins(b)).toBe('https://cdn.example.com https://img.example.net');
  });

  it('leaves out data URIs, whatever the case of the scheme', () => {
    const b = brandOf({
      [L]: 'data:image/png;base64,AAAA',
      [LD]: 'DATA:image/png;base64,AAAA',
      [I]: 'https://img.example.net/icon.svg',
    });
    expect(imageOrigins(b)).toBe('https://img.example.net');
  });

  it('is empty with nothing configured', () => {
    expect(imageOrigins(brandOf({}))).toBe('');
  });
});

describe('faviconTags', () => {
  it('is empty without an icon, even with a logo', () => {
    expect(faviconTags(brandOf({ [L]: 'https://cdn.example/l.svg' }))).toEqual([]);
  });

  it('is one link for an icon', () => {
    expect(faviconTags(brandOf({ [I]: 'https://cdn.example/i.svg' }))).toEqual([
      { rel: 'icon', href: 'https://cdn.example/i.svg' },
    ]);
  });

  it('adds a dark-scheme link after the base for a dark icon', () => {
    expect(
      faviconTags(brandOf({ [I]: 'https://cdn.example/i.svg', [ID]: 'https://cdn.example/d.svg' })),
    ).toEqual([
      { rel: 'icon', href: 'https://cdn.example/i.svg' },
      { rel: 'icon', href: 'https://cdn.example/d.svg', media: '(prefers-color-scheme: dark)' },
    ]);
  });
});
