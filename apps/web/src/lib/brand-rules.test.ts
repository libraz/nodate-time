import { describe, expect, it } from 'vitest';
import { DEFAULT_APP_NAME, logoImgSrc, resolveBrand } from './brand-rules';

// biome-ignore lint/style/useNamingConvention: the build variable's name
const name = (value: unknown) => resolveBrand({ VITE_APP_NAME: value }).appName;
// biome-ignore lint/style/useNamingConvention: the build variable's name
const logo = (value: unknown) => resolveBrand({ VITE_APP_LOGO_URL: value }).logoUrl;

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

describe('resolveBrand logo', () => {
  it('is absent when unset or blank', () => {
    expect(logo(undefined)).toBeUndefined();
    expect(logo('')).toBeUndefined();
    expect(logo('  ')).toBeUndefined();
  });

  it('accepts https URLs and image data URIs', () => {
    expect(logo('https://cdn.example.com/logo.svg')).toBe('https://cdn.example.com/logo.svg');
    expect(logo('data:image/svg+xml,%3Csvg%3E')).toBe('data:image/svg+xml,%3Csvg%3E');
    expect(logo('data:image/png;base64,AAAA')).toBe('data:image/png;base64,AAAA');
  });

  it('enforces the data URI length limit', () => {
    const prefix = 'data:image/png;base64,';
    expect(logo(prefix + 'A'.repeat(32_768 - prefix.length))).toHaveLength(32_768);
    expect(() => logo(prefix + 'A'.repeat(32_769 - prefix.length))).toThrow('VITE_APP_LOGO_URL');
  });

  it.each([
    ['http', 'http://cdn.example.com/logo.svg'],
    ['javascript', 'javascript:alert(1)'],
    ['relative path', '/logo.svg'],
    ['unparsable', 'https://'],
    ['non-image data URI', 'data:text/html,hi'],
  ])('rejects %s', (_label, value) => {
    expect(() => logo(value)).toThrow('VITE_APP_LOGO_URL');
  });
});

describe('logoImgSrc', () => {
  it('returns the https origin', () => {
    expect(logoImgSrc('https://cdn.example.com/a/logo.svg?v=1')).toBe('https://cdn.example.com');
  });

  it('returns an empty string for data URIs and no logo', () => {
    expect(logoImgSrc('data:image/png;base64,AAAA')).toBe('');
    expect(logoImgSrc(undefined)).toBe('');
  });
});
