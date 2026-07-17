import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { HistoryItem } from './history-timeline';

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn() },
}));

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: { locale: string }) => string) => selector({ locale: 'en' }),
}));

import { api } from '@/lib/api';
import { HistoryTimeline } from './history-timeline';

const mockApi = vi.mocked(api);

function item(id: number, summary: string, createdAt: string): HistoryItem {
  return {
    id,
    action: 'update',
    summary,
    createdAt,
    actor: { id: `u${id}`, name: `User ${id}`, icon: 'A' },
  };
}

afterEach(cleanup);

describe('HistoryTimeline', () => {
  it('renders a newest-first API response as an oldest-to-newest timeline', async () => {
    // API delivers newest-first.
    mockApi.get.mockResolvedValue([
      item(3, 'newest', '2026-04-22T10:00:00+09:00'),
      item(2, 'middle', '2026-04-21T10:00:00+09:00'),
      item(1, 'oldest', '2026-04-20T10:00:00+09:00'),
    ] as never);

    const { container } = render(
      <HistoryTimeline kind="event" calendarId="cal-1" entityId="evt-1" />,
    );

    await waitFor(() => expect(screen.getByText('oldest')).toBeInTheDocument());

    const text = container.textContent ?? '';
    expect(text.indexOf('oldest')).toBeLessThan(text.indexOf('middle'));
    expect(text.indexOf('middle')).toBeLessThan(text.indexOf('newest'));
  });
});
