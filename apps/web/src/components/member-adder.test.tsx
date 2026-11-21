import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/i18n', () => ({
  useT: () => (key: string, vars?: Record<string, string | number>) =>
    vars ? `${key}:${Object.values(vars).join(',')}` : key,
}));

vi.mock('@/lib/client', () => ({
  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn(), POST: vi.fn() },
}));

vi.mock('@/lib/api', () => ({
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
  isAbortError: (e: unknown) => e instanceof DOMException && e.name === 'AbortError',
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const fetchMembers = vi.fn();
vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: { fetchMembers: typeof fetchMembers }) => unknown) =>
    selector({ fetchMembers }),
}));

import { client } from '@/lib/client';
import { toast } from '@/lib/toast';
import { MemberAdder } from './member-adder';

const apiGet = vi.mocked(client.GET) as unknown as ReturnType<typeof vi.fn>;
const apiPost = vi.mocked(client.POST) as unknown as ReturnType<typeof vi.fn>;

const hanako = { id: 'u-1', name: 'Hanako', email: 'hanako@example.com' };
const taro = { id: 'u-2', name: 'Taro', email: 'taro@example.com' };

const onDone = vi.fn();
const onInviteByLink = vi.fn();

function renderAdder() {
  return render(<MemberAdder calendarId="cal-1" onDone={onDone} onInviteByLink={onInviteByLink} />);
}

beforeEach(() => {
  vi.useFakeTimers();
  apiGet.mockReset();
  apiPost.mockReset();
  fetchMembers.mockReset();
  onDone.mockReset();
  vi.mocked(toast.error).mockReset();
  vi.mocked(toast.success).mockReset();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

async function settle(ms = 0) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe('MemberAdder', () => {
  it('adds everyone chosen with the role beside the button, then hands back who was added', async () => {
    apiGet.mockResolvedValue({ data: { items: [hanako, taro], hasMore: false } });
    apiPost.mockResolvedValue({ data: {} });
    renderAdder();
    await settle();

    const submit = screen.getByRole('button', { name: 'members.addPick' });
    expect((submit as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(screen.getByRole('checkbox', { name: /Hanako/ }));
    fireEvent.click(screen.getByRole('checkbox', { name: /Taro/ }));
    fireEvent.click(screen.getByRole('radio', { name: 'members.roleViewer' }));
    fireEvent.click(screen.getByRole('button', { name: 'members.addSelected:2' }));
    await settle();

    expect(apiPost.mock.calls).toEqual([
      [
        '/calendars/{calendarId}/members',
        { params: { path: { calendarId: 'cal-1' } }, body: { userId: 'u-1', role: 'viewer' } },
      ],
      [
        '/calendars/{calendarId}/members',
        { params: { path: { calendarId: 'cal-1' } }, body: { userId: 'u-2', role: 'viewer' } },
      ],
    ]);
    expect(fetchMembers).toHaveBeenCalledWith('cal-1');
    expect(onDone).toHaveBeenCalledWith(['u-1', 'u-2']);
  });

  it('keeps a choice when a search hides the person', async () => {
    apiGet.mockResolvedValueOnce({ data: { items: [hanako, taro], hasMore: false } });
    apiGet.mockResolvedValue({ data: { items: [taro], hasMore: false } });
    renderAdder();
    await settle();

    fireEvent.click(screen.getByRole('checkbox', { name: /Hanako/ }));
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'ta' } });
    await settle(300);

    expect(screen.queryByRole('checkbox', { name: /Hanako/ })).toBeNull();
    expect(screen.getByRole('button', { name: 'members.addUnselect:Hanako' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'members.addSelected:1' })).toBeTruthy();
  });

  it('waits for typing to pause before searching, and searches only the last text', async () => {
    apiGet.mockResolvedValue({ data: { items: [], hasMore: false } });
    renderAdder();
    await settle();
    apiGet.mockClear();

    const box = screen.getByRole('searchbox');
    fireEvent.change(box, { target: { value: 'ha' } });
    await settle(100);
    fireEvent.change(box, { target: { value: 'han ' } });
    await settle(300);

    expect(apiGet).toHaveBeenCalledTimes(1);
    expect(apiGet.mock.calls[0]?.[0]).toBe('/calendars/{calendarId}/member-candidates');
    expect(apiGet.mock.calls[0]?.[1]).toMatchObject({
      params: { path: { calendarId: 'cal-1' }, query: { q: 'han' } },
    });
    expect(screen.getByText('members.addNoMatch')).toBeTruthy();
  });

  it('stays open with only the people still missing when some additions are refused', async () => {
    apiGet.mockResolvedValue({ data: { items: [hanako, taro], hasMore: false } });
    apiPost
      .mockResolvedValueOnce({ data: {} })
      .mockRejectedValueOnce(new Error('already a member'));
    renderAdder();
    await settle();

    fireEvent.click(screen.getByRole('checkbox', { name: /Hanako/ }));
    fireEvent.click(screen.getByRole('checkbox', { name: /Taro/ }));
    fireEvent.click(screen.getByRole('button', { name: 'members.addSelected:2' }));
    await settle();

    expect(toast.error).toHaveBeenCalledWith('members.addPartial:1,already a member');
    expect(onDone).not.toHaveBeenCalled();
    expect(fetchMembers).toHaveBeenCalledWith('cal-1');
    expect(screen.queryByRole('checkbox', { name: /Hanako/ })).toBeNull();
    expect(screen.getByRole('button', { name: 'members.addSelected:1' })).toBeTruthy();
  });

  it('says when the page is not everyone, and points outsiders to a link', async () => {
    apiGet.mockResolvedValue({ data: { items: [hanako], hasMore: true } });
    renderAdder();
    await settle();

    expect(screen.getByText('members.addMore')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'members.addViaLink' }));
    expect(onInviteByLink).toHaveBeenCalled();
  });
});
