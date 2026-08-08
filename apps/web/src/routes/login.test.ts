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
    expect(oauthErrorMessageKey('oauth_email_unverified')).toBe('auth.oauthEmailUnverified');
    expect(oauthErrorMessageKey('oauth_email_unsupported')).toBe('auth.oauthEmailUnsupported');
  });

  // The fallback is why an unmapped code is easy to miss: it reads as an
  // ordinary sign-in failure rather than as a missing case, so a reason the
  // server took trouble to distinguish arrives as "try again".
  it('does not leave a known code on the generic message', () => {
    expect(oauthErrorMessageKey('oauth_email_unsupported')).not.toBe('auth.oauthFailed');
  });

  it('falls back to the generic failure message for an unknown code', () => {
    expect(oauthErrorMessageKey('something_else')).toBe('auth.oauthFailed');
  });
});
