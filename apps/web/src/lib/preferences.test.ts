import { describe, expect, it } from 'vitest';
import { accountPreferences, detectTimezone } from './preferences';

describe('accountPreferences', () => {
  it('reads what the account stores', () => {
    expect(accountPreferences({ locale: 'en', timezone: 'Europe/Paris' })).toEqual({
      locale: 'en',
      timezone: 'Europe/Paris',
    });
  });

  it('drops a locale this build has no translations for', () => {
    // Adopting it would leave every label rendering as its own key.
    expect(accountPreferences({ locale: 'fr' })).toEqual({});
  });

  it('drops a timezone this runtime cannot resolve', () => {
    // Every date format would throw on it, which is worse than the wrong zone.
    expect(accountPreferences({ timezone: 'Mars/Olympus_Mons' })).toEqual({});
  });

  it('reports nothing for an account that stores nothing', () => {
    expect(accountPreferences({})).toEqual({});
  });
});

describe('detectTimezone', () => {
  it('always names a zone', () => {
    expect(detectTimezone()).toMatch(/^[A-Za-z]+(\/[A-Za-z_+\-0-9]+)*$/);
  });
});
