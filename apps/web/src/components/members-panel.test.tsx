import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Member } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
  getT: () => (key: string) => key,
}));

vi.mock('@/lib/api', () => ({
  api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() },
  errorMessage: (e: unknown) => (e instanceof Error ? e.message : 'error'),
}));

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

const uiState = {
  rightPanel: 'members' as string | null,
  toggleRightPanel: vi.fn(),
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
    avatar: 'https://storage.example/avatars/u1?signature=abc',
  },
  {
    id: 'u2',
    name: 'Taro',
    email: 'taro@example.com',
    role: 'owner',
    color: '#00ff00',
    avatar: 'https://storage.example/avatars/u2?signature=def',
  },
  { id: 'u3', name: 'Jiro', email: 'jiro@example.com', role: 'writer', color: '#0000ff' },
];

const calendarState = {
  calendars: [
    {
      id: 'cal-1',
      name: 'Team',
      color: '#000',
      coverUrl: '',
      createdAt: '',
      publicShared: false,
      role: 'reader',
      memberColor: '#ff0000',
    },
  ],
  activeCalendarIds: ['cal-1'],
  membersMap: { 'cal-1': members },
  fetchMembers: vi.fn(),
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

import { MembersPanel } from './members-panel';

beforeEach(() => {
  uiState.rightPanel = 'members';
});

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
    expect(sources).toEqual([members[0]?.avatar, members[1]?.avatar]);
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
    const focusable = Array.from(panel.querySelectorAll<HTMLElement>('button,input,select'));
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
