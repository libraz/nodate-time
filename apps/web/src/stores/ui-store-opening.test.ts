import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// 2026-11-01 01:30 in Tokyo, still 2026-10-31 on a UTC device.
const INSTANT = new Date('2026-10-31T16:30:00Z');

async function freshStore(storedZone?: string) {
  vi.resetModules();
  localStorage.clear();
  if (storedZone) localStorage.setItem('tt_timezone', JSON.stringify(storedZone));
  return (await import('./ui-store')).useUiStore;
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(INSTANT);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('opening day and month', () => {
  it('are today in the stored timezone, not the device date', async () => {
    const store = await freshStore('Asia/Tokyo');
    const s = store.getState();
    expect(s.selectedDate.toISODate()).toBe('2026-11-01');
    expect(s.currentMonth.toISODate()).toBe('2026-11-01');
  });

  it('follow an account timezone adopted before the user navigated', async () => {
    const store = await freshStore('America/Los_Angeles');
    expect(store.getState().selectedDate.toISODate()).toBe('2026-10-31');

    store.getState().adoptAccountPreferences({ timezone: 'Asia/Tokyo' });

    const s = store.getState();
    expect(s.selectedDate.toISODate()).toBe('2026-11-01');
    expect(s.currentMonth.toISODate()).toBe('2026-11-01');
  });

  it('stay where the user navigated when the account timezone arrives', async () => {
    const store = await freshStore('America/Los_Angeles');
    store.getState().navigateMonth(-3);
    const month = store.getState().currentMonth.toISODate();

    store.getState().adoptAccountPreferences({ timezone: 'Asia/Tokyo' });

    expect(store.getState().currentMonth.toISODate()).toBe(month);
  });

  it('are re-derived from the account timezone when the session resets', async () => {
    const store = await freshStore('Asia/Tokyo');
    store.getState().navigateMonth(5);

    store.getState().resetSessionUi();

    expect(store.getState().currentMonth.toISODate()).toBe('2026-11-01');
    expect(store.getState().selectedDate.toISODate()).toBe('2026-11-01');
  });
});
