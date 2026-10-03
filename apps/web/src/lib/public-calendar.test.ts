import { describe, expect, it } from 'vitest';
import { shareFailureOf } from './public-calendar';

describe('shareFailureOf', () => {
  it('separates a link that is gone from a server that is busy', () => {
    // The owner's reaction to each is different, and only one of them is
    // right: told the link expired, they issue a new one and every embed
    // already published against the old one stops working.
    expect(shareFailureOf({ status: 404 })).toBe('gone');
    expect(shareFailureOf({ status: 410 })).toBe('gone');
    expect(shareFailureOf({ status: 429 })).toBe('busy');
    expect(shareFailureOf({ status: 500 })).toBe('busy');
    expect(shareFailureOf({ status: 502 })).toBe('busy');
  });

  it('treats a failure with nothing to go on as busy', () => {
    // A dropped connection says nothing about the link, and guessing that it
    // died is the guess with a destructive fix attached.
    expect(shareFailureOf(new Error('network'))).toBe('busy');
    expect(shareFailureOf(null)).toBe('busy');
    expect(shareFailureOf(undefined)).toBe('busy');
  });
});
