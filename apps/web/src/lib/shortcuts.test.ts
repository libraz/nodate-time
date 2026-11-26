import { cleanup, fireEvent, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useUiStore } from '@/stores/ui-store';
import { useCalendarShortcuts } from './shortcuts';

/** A button in the document, optionally inside a container with the given role. */
function mountButton(role?: string): HTMLButtonElement {
  const button = document.createElement('button');
  button.type = 'button';
  const host = document.createElement('div');
  if (role) host.setAttribute('role', role);
  host.appendChild(button);
  document.body.appendChild(host);
  return button;
}

beforeEach(() => {
  useUiStore.setState({ calendarView: 'month' });
});

afterEach(() => {
  cleanup();
  document.body.replaceChildren();
});

describe('useCalendarShortcuts', () => {
  it('answers a key pressed on the calendar', () => {
    const button = mountButton();
    renderHook(() => useCalendarShortcuts(true));

    fireEvent.keyDown(button, { key: 'w' });

    expect(useUiStore.getState().calendarView).toBe('week');
  });

  // The desktop event panel is a dialog without aria-modal: while focus is in
  // it, its keys are its own even though the calendar beside it stays live.
  it('leaves a key pressed inside a non-modal dialog to the dialog', () => {
    const button = mountButton('dialog');
    renderHook(() => useCalendarShortcuts(true));

    fireEvent.keyDown(button, { key: 'w' });

    expect(useUiStore.getState().calendarView).toBe('month');
  });
});
