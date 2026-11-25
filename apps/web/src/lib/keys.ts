import type { KeyboardEvent } from 'react';

/**
 * Reports whether a keydown is an Enter meant to submit. The Enter that
 * commits an IME conversion is not one: Safari reports it with keyCode 229
 * rather than isComposing.
 */
export function isSubmitEnter(e: KeyboardEvent): boolean {
  return e.key === 'Enter' && !e.nativeEvent.isComposing && e.keyCode !== 229;
}
