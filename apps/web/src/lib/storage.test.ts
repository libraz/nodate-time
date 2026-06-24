import { afterEach, describe, expect, it } from 'vitest';
import { loadJson, removeItem, saveJson } from './storage';

afterEach(() => localStorage.clear());

describe('saveJson / loadJson', () => {
  it('round-trips a value through localStorage', () => {
    saveJson('greeting', { hello: 'world' });
    expect(loadJson('greeting', null)).toEqual({ hello: 'world' });
  });

  it('namespaces keys with the tt_ prefix', () => {
    saveJson('view', 'month');
    expect(localStorage.getItem('tt_view')).toBe('"month"');
  });

  it('returns the fallback when the key is missing', () => {
    expect(loadJson('absent', 'fallback')).toBe('fallback');
  });

  it('returns the fallback when stored JSON is corrupt', () => {
    localStorage.setItem('tt_broken', '{not valid json');
    expect(loadJson('broken', 42)).toBe(42);
  });

  it('preserves arrays and primitives', () => {
    saveJson('ids', ['a', 'b']);
    expect(loadJson<string[]>('ids', [])).toEqual(['a', 'b']);
    saveJson('count', 7);
    expect(loadJson('count', 0)).toBe(7);
  });
});

describe('removeItem', () => {
  it('deletes a stored value', () => {
    saveJson('temp', 'x');
    removeItem('temp');
    expect(loadJson('temp', 'gone')).toBe('gone');
  });
});
