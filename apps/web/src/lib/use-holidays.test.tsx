import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

// The countries loader shares this module but not this hook; it is stubbed so
// the mock covers every name the module under test imports.
vi.mock('@/lib/holidays', () => ({
  preloadHolidays: vi.fn(() => Promise.resolve()),
  fallbackHolidayCountries: vi.fn(() => []),
  loadHolidayCountries: vi.fn(() => Promise.resolve([])),
}));

import { preloadHolidays } from '@/lib/holidays';
import { useHolidayLoader } from './use-holidays';

const mockPreload = vi.mocked(preloadHolidays);

function Probe({ country, years }: { country: string | null; years: number[] }) {
  const revision = useHolidayLoader(country, years);
  return <span data-testid="revision">{revision}</span>;
}

function revisionText(): string {
  return screen.getByTestId('revision').textContent ?? '';
}

afterEach(() => {
  cleanup();
  mockPreload.mockReset();
  mockPreload.mockResolvedValue(undefined);
});

describe('useHolidayLoader', () => {
  it('changes once the data has arrived', async () => {
    render(<Probe country="JP" years={[2026]} />);

    expect(revisionText()).toBe('0');

    await act(async () => {});

    expect(revisionText()).toBe('1');
  });

  // The years reach the hook as a fresh array on every render, so the request
  // is keyed by what they say rather than by the array's identity.
  it('asks for each year once, whatever order they arrive in', async () => {
    render(<Probe country="JP" years={[2026, 2025, 2026]} />);
    await act(async () => {});

    expect(mockPreload).toHaveBeenCalledTimes(1);
    expect(mockPreload).toHaveBeenCalledWith('JP', [2025, 2026]);
  });

  it('does not ask again when the same years come back in a new array', async () => {
    const { rerender } = render(<Probe country="JP" years={[2025, 2026]} />);
    await act(async () => {});

    rerender(<Probe country="JP" years={[2026, 2025]} />);
    await act(async () => {});

    expect(mockPreload).toHaveBeenCalledTimes(1);
  });

  it('asks again when the country changes', async () => {
    const { rerender } = render(<Probe country="JP" years={[2026]} />);
    await act(async () => {});

    rerender(<Probe country="DE" years={[2026]} />);
    await act(async () => {});

    expect(mockPreload).toHaveBeenNthCalledWith(2, 'DE', [2026]);
    expect(revisionText()).toBe('2');
  });

  // A view with no country configured draws no holidays, and asking for none is
  // still an answer: the revision has to settle rather than stay pending.
  it('reports a load for a caller that named no country', async () => {
    render(<Probe country={null} years={[]} />);
    await act(async () => {});

    expect(mockPreload).toHaveBeenCalledWith(null, []);
    expect(revisionText()).toBe('1');
  });
});
