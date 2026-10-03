import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { en } from '@/i18n/en';
import { ja } from '@/i18n/ja';
import { useAuthStore } from './auth-store';
import { useUiStore } from './ui-store';

function rejection(status: number, code: string, message: string): Response {
  return new Response(JSON.stringify({ status, code, message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const fetchMock = vi.fn();

beforeEach(() => {
  localStorage.clear();
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  useAuthStore.setState({ error: null, isLoading: false });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

// The server's sentence is English whatever the reader chose; the code is
// what carries the meaning across languages.
describe('sign-in errors', () => {
  it.each([
    ['ja', ja],
    ['en', en],
  ] as const)('reads a rejected sign-in in the %s locale', async (locale, dict) => {
    useUiStore.setState({ locale });
    fetchMock.mockResolvedValue(rejection(401, 'AUTH.BAD_CREDENTIALS', 'Invalid credentials'));

    await expect(useAuthStore.getState().login('a@b.c', 'bad')).rejects.toBeTruthy();

    expect(useAuthStore.getState().error).toBe(dict['apiError.AUTH.BAD_CREDENTIALS']);
  });

  it('reads a rejected registration in the reader locale', async () => {
    useUiStore.setState({ locale: 'ja' });
    fetchMock.mockResolvedValue(rejection(409, 'AUTH.EMAIL_EXISTS', 'Email already registered'));

    await expect(useAuthStore.getState().register('A', 'a@b.c', 'pw')).rejects.toBeTruthy();

    expect(useAuthStore.getState().error).toBe(ja['apiError.AUTH.EMAIL_EXISTS']);
  });
});
