import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/client';
import type { Label } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string, params?: Record<string, string>) =>
    params?.name ? `${key}:${params.name}` : key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const live: Label = { id: 'l1', name: 'Live', color: '#E73B3B', etag: '"l1"' };
const lesson: Label = { id: 'l2', name: 'Lesson', color: '#2ECC87', etag: '"l2"' };

const uiState = {
  rightPanel: 'labels' as string | null,
  toggleRightPanel: vi.fn(),
  panelCalendarRequest: null as { calendarId: string } | null,
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const calendarState = {
  calendars: [
    {
      id: 'cal-1',
      name: 'Team',
      color: '#000',
      createdAt: '',
      publicShared: false,
      role: 'manager',
      memberColor: '#ff0000',
    },
  ],
  activeCalendarIds: ['cal-1'],
  labelsMap: {} as Record<string, Label[]>,
  labelErrors: {} as Record<string, string>,
  fetchLabels: vi.fn(),
  addLabel: vi.fn(),
  updateLabel: vi.fn(),
  deleteLabel: vi.fn(),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

import { toast } from '@/lib/toast';
import { LabelsPanel } from './labels-panel';

/** Opens the panel as a member holding `role` on the one calendar shown. */
function renderPanel(role: string) {
  for (const c of calendarState.calendars) c.role = role;
  return render(<LabelsPanel />);
}

beforeEach(() => {
  uiState.rightPanel = 'labels';
  calendarState.labelsMap = { 'cal-1': [live, lesson] };
  calendarState.labelErrors = {};
  calendarState.addLabel.mockResolvedValue(undefined);
  calendarState.updateLabel.mockResolvedValue(undefined);
  calendarState.deleteLabel.mockResolvedValue(undefined);
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  document.body.style.overflow = '';
});

describe('LabelsPanel', () => {
  it('lists the labels for everyone but offers no editing to a reader', () => {
    renderPanel('reader');

    expect(screen.getByText('Live')).toBeInTheDocument();
    expect(screen.getByText('Lesson')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'labels.add' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'labels.edit:Live' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'labels.delete:Live' })).toBeNull();
  });

  it('asks for the calendar labels when it opens', () => {
    renderPanel('reader');

    expect(calendarState.fetchLabels).toHaveBeenCalledWith('cal-1');
  });

  it('adds a label from the name and colour, trimming the name', async () => {
    renderPanel('manager');

    fireEvent.click(screen.getByRole('button', { name: 'labels.add' }));
    fireEvent.change(screen.getByLabelText('labels.name'), { target: { value: '  Shoot ' } });
    // The header's add button gives way to the form, so the one left is the form's.
    fireEvent.click(screen.getByRole('button', { name: 'labels.add' }));

    await waitFor(() => expect(calendarState.addLabel).toHaveBeenCalled());
    expect(calendarState.addLabel).toHaveBeenCalledWith('cal-1', {
      name: 'Shoot',
      color: expect.stringMatching(/^#[0-9A-F]{6}$/),
    });
  });

  it('does not submit an empty name', () => {
    renderPanel('manager');

    fireEvent.click(screen.getByRole('button', { name: 'labels.add' }));

    expect(screen.getByRole('button', { name: 'labels.add' })).toBeDisabled();
  });

  it('renames a label and sends only what changed', async () => {
    renderPanel('manager');

    fireEvent.click(screen.getByRole('button', { name: 'labels.edit:Live' }));
    fireEvent.change(screen.getByLabelText('labels.name'), { target: { value: 'Gig' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(calendarState.updateLabel).toHaveBeenCalled());
    expect(calendarState.updateLabel).toHaveBeenCalledWith('cal-1', 'l1', { name: 'Gig' });
  });

  it('asks before deleting, then deletes', async () => {
    renderPanel('manager');

    fireEvent.click(screen.getByRole('button', { name: 'labels.delete:Live' }));
    expect(screen.getByText('labels.deleteConfirm')).toBeInTheDocument();
    expect(calendarState.deleteLabel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'common.delete' }));

    await waitFor(() => expect(calendarState.deleteLabel).toHaveBeenCalledWith('cal-1', 'l1'));
  });

  it('reports a refused write and reads the list again on a stale version', async () => {
    calendarState.updateLabel.mockRejectedValueOnce(new ApiError(412, 'stale', 'LABEL.STALE'));
    renderPanel('manager');
    calendarState.fetchLabels.mockClear();

    fireEvent.click(screen.getByRole('button', { name: 'labels.edit:Live' }));
    fireEvent.change(screen.getByLabelText('labels.name'), { target: { value: 'Gig' } });
    fireEvent.click(screen.getByRole('button', { name: 'common.save' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    expect(calendarState.fetchLabels).toHaveBeenCalledWith('cal-1');
  });

  it('offers a retry when the list could not be loaded', () => {
    calendarState.labelsMap = {};
    calendarState.labelErrors = { 'cal-1': 'down' };
    renderPanel('manager');
    calendarState.fetchLabels.mockClear();

    fireEvent.click(screen.getByRole('button', { name: 'error.retry' }));

    expect(calendarState.fetchLabels).toHaveBeenCalledWith('cal-1');
  });

  it('draws nothing while the panel is closed', () => {
    uiState.rightPanel = null;
    const { container } = renderPanel('manager');

    expect(container).toBeEmptyDOMElement();
  });

  it('closes as the labels panel', () => {
    renderPanel('reader');

    fireEvent.click(screen.getAllByRole('button', { name: 'common.close' }).at(-1) as HTMLElement);

    expect(uiState.toggleRightPanel).toHaveBeenCalledWith('labels');
  });
});
