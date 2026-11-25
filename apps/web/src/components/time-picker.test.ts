import { cleanup } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { parseTypedTime } from './time-picker';

// Mock the ui-store to provide locale
vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => string) => selector({ locale: 'en' }),
}));

afterEach(cleanup);

describe('parseTypedTime', () => {
  it.each([
    ['9', '09:00'],
    ['09', '09:00'],
    ['9:30', '09:30'],
    ['09:30', '09:30'],
    ['930', '09:30'],
    ['0930', '09:30'],
    ['0', '00:00'],
    ['23:59', '23:59'],
    ['\uFF19\uFF1A\uFF13\uFF10', '09:30'],
    [' 14:05 ', '14:05'],
  ])('reads %j as %s', (input, expected) => {
    expect(parseTypedTime(input)).toBe(expected);
  });

  it.each(['24:00', '9:5', '960', '', '12345', 'ab', '9:60', '25'])('refuses %j', (input) => {
    expect(parseTypedTime(input)).toBeNull();
  });
});
