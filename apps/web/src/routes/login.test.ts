import { describe, expect, it } from 'vitest';
import { oauthErrorMessageKey } from './login';

describe('oauthErrorMessageKey', () => {
  it('returns null when no error code is present', () => {
    expect(oauthErrorMessageKey(undefined)).toBeNull();
  });

  it('maps each known OAuth error code to its message key', () => {
    expect(oauthErrorMessageKey('oauth_denied')).toBe('auth.oauthDenied');
    expect(oauthErrorMessageKey('oauth_failed')).toBe('auth.oauthFailed');
    expect(oauthErrorMessageKey('oauth_state')).toBe('auth.oauthState');
    expect(oauthErrorMessageKey('oauth_not_allowed')).toBe('auth.oauthNotAllowed');
  });

  it('falls back to the generic failure message for an unknown code', () => {
    expect(oauthErrorMessageKey('something_else')).toBe('auth.oauthFailed');
  });
});
