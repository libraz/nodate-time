import type { KeyboardEvent } from 'react';

/**
 * Reports whether a keydown is an Enter meant to submit. The Enter that
 * commits an IME conversion is not one: Safari reports it with keyCode 229
 * rather than isComposing.
 */
export function isSubmitEnter(e: KeyboardEvent): boolean {
  return e.key === 'Enter' && !e.nativeEvent.isComposing && e.keyCode !== 229;
}

/** Reports whether a keydown is Cmd/Ctrl+Enter, the shortcut that submits a form from any field. */
export function isSubmitShortcut(e: KeyboardEvent): boolean {
  return isSubmitEnter(e) && (e.metaKey || e.ctrlKey);
}

/** The modifier key the submit shortcut is labelled with on this platform. */
export function submitModifierLabel(): string {
  return /Mac|iPhone|iPad|iPod/.test(navigator.userAgent) ? '\u2318' : 'Ctrl';
}
