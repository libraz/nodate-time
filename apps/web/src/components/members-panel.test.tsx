import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@tanstack/react-router', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exported component name
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}));

import type { Member } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/client')>()),
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
  isAbortError: () => false,

  // biome-ignore lint/style/useNamingConvention: mirrors the client's HTTP method names
  client: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() },
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const uiState = {
  rightPanel: 'members' as string | null,
  toggleRightPanel: vi.fn(),
  openPanelFor: vi.fn(),
  panelCalendarRequest: null as { calendarId: string; addMembers?: boolean } | null,
  locale: 'ja',
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

const members: Member[] = [
  {
    id: 'u1',
    name: 'Hanako',
    email: 'hanako@example.com',
    role: 'reader',
    color: '#ff0000',
    avatarUrl: 'https://storage.example/avatars/u1?signature=abc',
  },
  {
    id: 'u2',
    name: 'Taro',
    email: 'taro@example.com',
    role: 'owner',
    color: '#00ff00',
    avatarUrl: 'https://storage.example/avatars/u2?signature=def',
  },
  { id: 'u3', name: 'Jiro', email: 'jiro@example.com', role: 'writer', color: '#0000ff' },
];

const calendarState = {
  calendars: [
    {
      id: 'cal-1',
      name: 'Team',
      color: '#000',
      createdAt: '',
      publicShared: false,
      role: 'reader',
      memberColor: '#ff0000',
    },
  ],
  activeCalendarIds: ['cal-1'],
  membersMap: { 'cal-1': members } as Record<string, Member[]>,
  memberErrors: {} as Record<string, string>,
  fetchMembers: vi.fn(),
  leaveCalendar: vi.fn(),
  updateMemberColor: vi.fn(),
};

vi.mock('@/stores/calendar-store', () => ({
  useCalendarStore: (selector: (s: typeof calendarState) => unknown) => selector(calendarState),
}));

// The viewer is a reader, so their own row carries the colour picker and the
// other rows are drawn plain: both paths render an avatar.
const authState = { user: { id: 'u1', name: 'Hanako' } };

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector: (s: typeof authState) => unknown) => selector(authState),
}));

import { client } from '@/lib/client';
import { MembersPanel } from './members-panel';

beforeEach(() => {
  uiState.rightPanel = 'members';
  uiState.panelCalendarRequest = null;
  setMyRole('reader');
  calendarState.membersMap = { 'cal-1': members };
  calendarState.memberErrors = {};
});

function setMyRole(role: string) {
  const cal = calendarState.calendars[0];
  if (cal) cal.role = role;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.restoreAllMocks();
  document.body.style.overflow = '';
});

/** jsdom reports no layout, so focusable elements have to look painted. */
function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

describe('MembersPanel', () => {
  // The list drew every member as the initial of their name, so an uploaded
  // picture existed on the server and appeared nowhere in the app.
  it('shows the picture a member uploaded, on their own row and on others', () => {
    const { container } = render(<MembersPanel />);

    const sources = Array.from(container.querySelectorAll('img')).map((img) =>
      img.getAttribute('src'),
    );
    expect(sources).toEqual([members[0]?.avatarUrl, members[1]?.avatarUrl]);
  });

  it('stands in with the initial for a member who has no picture', () => {
    render(<MembersPanel />);

    expect(screen.getByText('J')).toBeInTheDocument();
  });
});

// The panel covers the page but had none of what that owes the keyboard: Tab
// walked into the calendar behind it and Escape did nothing.
describe('MembersPanel keyboard', () => {
  it('locks the page behind it and closes on Escape', () => {
    render(<MembersPanel />);

    expect(document.body.style.overflow).toBe('hidden');

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(uiState.toggleRightPanel).toHaveBeenCalledWith('members');
  });

  it('keeps Tab inside the panel', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    render(<MembersPanel />);

    const panel = await screen.findByRole('dialog');
    const focusable = Array.from(
      panel.querySelectorAll<HTMLElement>('a[href],button,input,select'),
    );
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    expect(first).toBeDefined();
    expect(last).toBeDefined();
    if (!first || !last) return;

    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
  });

  it('returns focus to whatever opened it', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue(rectList());
    uiState.rightPanel = null;
    function Harness() {
      return (
        <>
          <button type="button" data-testid="trigger">
            open
          </button>
          <MembersPanel />
        </>
      );
    }
    const { rerender } = render(<Harness />);
    const trigger = screen.getByTestId('trigger');
    trigger.focus();

    uiState.rightPanel = 'members';
    rerender(<Harness />);
    await waitFor(() => expect(document.activeElement).not.toBe(trigger));

    uiState.rightPanel = null;
    rerender(<Harness />);

    expect(document.activeElement).toBe(trigger);
  });
});

describe('MembersPanel load state', () => {
  it('shows a placeholder while members have neither loaded nor failed', () => {
    calendarState.membersMap = {};
    const { container } = render(<MembersPanel />);

    expect(container.querySelector('[aria-busy="true"]')).not.toBeNull();
    expect(screen.queryByText('members.loadFailed')).toBeNull();
  });

  it('says the load failed and retries on request', () => {
    calendarState.membersMap = {};
    calendarState.memberErrors = { 'cal-1': 'boom' };
    render(<MembersPanel />);

    expect(screen.getByText('members.loadFailed')).toBeInTheDocument();
    calendarState.fetchMembers.mockClear();
    fireEvent.click(screen.getByRole('button', { name: 'error.retry' }));
    expect(calendarState.fetchMembers).toHaveBeenCalledWith('cal-1');
  });

  it('lists the members once loaded', () => {
    render(<MembersPanel />);

    expect(screen.getByText('Taro')).toBeInTheDocument();
    expect(screen.queryByText('members.loadFailed')).toBeNull();
  });
});

describe('MembersPanel membership', () => {
  // Settings let a member leave; the panel listing the same members did not
  // offer it at all.
  it('lets a member leave the calendar from their own row', async () => {
    calendarState.leaveCalendar.mockResolvedValue(undefined);
    render(<MembersPanel />);

    fireEvent.click(screen.getByRole('button', { name: 'members.leave' }));

    const sheet = screen.getByRole('alertdialog');
    expect(within(sheet).getByText('members.leaveConfirm')).toBeInTheDocument();
    expect(calendarState.leaveCalendar).not.toHaveBeenCalled();
    fireEvent.click(within(sheet).getByRole('button', { name: 'members.leave' }));
    await waitFor(() => expect(calendarState.leaveCalendar).toHaveBeenCalledWith('cal-1', 'u1'));
  });

  // A drag reports every step; only letting go is a choice, so it is one PUT.
  it('saves a colour dragged on the picker once, when it is let go', async () => {
    calendarState.updateMemberColor.mockResolvedValue(undefined);
    render(<MembersPanel />);
    fireEvent.click(screen.getByRole('button', { name: 'members.color: Hanako' }));
    fireEvent.click(screen.getByRole('button', { name: /members\.colorCustom/ }));
    const area = screen.getByRole('slider', { name: 'members.colorShade' });
    area.getBoundingClientRect = () =>
      ({ left: 0, top: 0, width: 100, height: 100, right: 100, bottom: 100 }) as DOMRect;

    fireEvent.pointerDown(area, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerMove(area, { clientX: 50, clientY: 50, pointerId: 1 });
    fireEvent.pointerMove(area, { clientX: 100, clientY: 50, pointerId: 1 });
    expect(calendarState.updateMemberColor).not.toHaveBeenCalled();
    expect(screen.getByLabelText('members.colorCode')).toHaveValue('#800000');

    fireEvent.pointerUp(area, { pointerId: 1 });

    await waitFor(() => expect(calendarState.updateMemberColor).toHaveBeenCalledTimes(1));
    expect(calendarState.updateMemberColor).toHaveBeenCalledWith('cal-1', 'u1', '#800000');
  });

  it('saves keyboard steps on the hue once they settle', async () => {
    vi.useFakeTimers();
    try {
      calendarState.updateMemberColor.mockResolvedValue(undefined);
      render(<MembersPanel />);
      fireEvent.click(screen.getByRole('button', { name: 'members.color: Hanako' }));
      fireEvent.click(screen.getByRole('button', { name: /members\.colorCustom/ }));
      const hue = screen.getByRole('slider', { name: 'members.colorHue' });

      fireEvent.keyDown(hue, { key: 'ArrowRight', shiftKey: true });
      fireEvent.keyDown(hue, { key: 'ArrowRight', shiftKey: true });
      expect(calendarState.updateMemberColor).not.toHaveBeenCalled();

      await act(async () => {
        vi.advanceTimersByTime(700);
      });
      expect(calendarState.updateMemberColor).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it('saves a palette colour in one click, even one another member has', async () => {
    calendarState.updateMemberColor.mockResolvedValue(undefined);
    render(<MembersPanel />);
    fireEvent.click(screen.getByRole('button', { name: 'members.color: Hanako' }));

    fireEvent.click(screen.getByRole('button', { name: '#47B2F7' }));

    await waitFor(() =>
      expect(calendarState.updateMemberColor).toHaveBeenCalledWith('cal-1', 'u1', '#47B2F7'),
    );
  });

  it('marks a palette colour another member already carries', () => {
    const taro = members[1] as Member;
    const before = taro.color;
    taro.color = '#F35F8C';
    try {
      render(<MembersPanel />);
      fireEvent.click(screen.getByRole('button', { name: 'members.color: Hanako' }));

      expect(
        screen.getByRole('button', { name: '#F35F8C — members.colorInUse: Taro' }),
      ).toBeInTheDocument();
      expect(screen.getByRole('button', { name: '#47B2F7' })).toBeInTheDocument();
    } finally {
      taro.color = before;
    }
  });

  it('takes a colour typed as R, G, B', async () => {
    calendarState.updateMemberColor.mockResolvedValue(undefined);
    render(<MembersPanel />);
    fireEvent.click(screen.getByRole('button', { name: 'members.color: Hanako' }));
    fireEvent.click(screen.getByRole('button', { name: /members\.colorCustom/ }));
    const field = screen.getByLabelText('members.colorCode');

    fireEvent.change(field, { target: { value: '0, 128, 255' } });
    fireEvent.keyDown(field, { key: 'Enter' });

    await waitFor(() =>
      expect(calendarState.updateMemberColor).toHaveBeenCalledWith('cal-1', 'u1', '#0080FF'),
    );
  });

  it('refuses text that is not a colour without saving', () => {
    render(<MembersPanel />);
    fireEvent.click(screen.getByRole('button', { name: 'members.color: Hanako' }));
    fireEvent.click(screen.getByRole('button', { name: /members\.colorCustom/ }));
    const field = screen.getByLabelText('members.colorCode');

    fireEvent.change(field, { target: { value: 'red' } });
    fireEvent.click(screen.getByRole('button', { name: 'members.colorApply' }));

    expect(field).toHaveAttribute('aria-invalid', 'true');
    expect(calendarState.updateMemberColor).not.toHaveBeenCalled();
  });
});

describe('MembersPanel adding people', () => {
  it('offers a reader no way to add anyone', () => {
    render(<MembersPanel />);
    expect(screen.queryByRole('button', { name: 'members.addShort' })).toBeNull();
  });

  it('swaps the list for the picker and back', async () => {
    setMyRole('owner');
    vi.mocked(client.GET).mockResolvedValue({ data: { items: [], hasMore: false } } as never);
    render(<MembersPanel />);

    fireEvent.click(screen.getByRole('button', { name: 'members.addShort' }));
    expect(screen.getByRole('searchbox')).toBeInTheDocument();
    expect(screen.queryByText('Jiro')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'members.backToList' }));
    expect(screen.getByText('Jiro')).toBeInTheDocument();
    await act(async () => {});
  });

  it('opens on the picker when another panel asks for it', async () => {
    setMyRole('owner');
    vi.mocked(client.GET).mockResolvedValue({ data: { items: [], hasMore: false } } as never);
    uiState.panelCalendarRequest = { calendarId: 'cal-1', addMembers: true };
    render(<MembersPanel />);

    expect(screen.getByRole('searchbox')).toBeInTheDocument();
    await act(async () => {});
  });
});
