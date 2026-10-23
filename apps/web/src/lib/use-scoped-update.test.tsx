import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { CalendarEvent } from '@/types/calendar';
import { useScopedUpdate } from './use-scoped-update';

const recurring = {
  id: 'evt-1_20260803',
  calendarId: 'cal-1',
  title: 'Standup',
  recurrenceRule: 'FREQ=WEEKLY',
} as unknown as CalendarEvent;

let request: ReturnType<typeof useScopedUpdate>['requestUpdate'] = () => {};

function Host() {
  const { requestUpdate, dialog } = useScopedUpdate();
  request = requestUpdate;
  return <>{dialog}</>;
}

describe('useScopedUpdate chooser', () => {
  it('is a named modal dialog that Escape dismisses', () => {
    render(<Host />);
    act(() => request(recurring, { title: 'Standup' } as never));

    const dialog = screen.getByRole('dialog');
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(dialog.getAttribute('aria-label')).toBeTruthy();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
