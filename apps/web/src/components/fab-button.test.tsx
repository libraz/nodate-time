import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MobileTab } from '@/stores/ui-store';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

const uiState = {
  openEventModal: vi.fn(),
  mobileTab: 'calendar' as MobileTab,
};

vi.mock('@/stores/ui-store', () => ({
  useUiStore: (selector: (s: typeof uiState) => unknown) => selector(uiState),
}));

import { FabButton } from './fab-button';

beforeEach(() => {
  uiState.mobileTab = 'calendar';
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('FabButton', () => {
  it('offers a new event on the calendar tab', () => {
    render(<FabButton />);

    expect(screen.getByLabelText('event.addEvent')).toBeInTheDocument();
  });

  // The button floats above whatever tab is showing, so on memo it covered
  // that tab's own add button with a control for a screen nobody was on.
  it('stays away from the tabs a new event means nothing on', () => {
    for (const tab of ['memo', 'search', 'settings'] as const) {
      uiState.mobileTab = tab;
      const { unmount } = render(<FabButton />);

      expect(screen.queryByLabelText('event.addEvent')).toBeNull();
      unmount();
    }
  });
});
