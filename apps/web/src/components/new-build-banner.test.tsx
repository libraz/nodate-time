import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useBuildStore } from '@/lib/build-check';
import { NewBuildBanner } from './new-build-banner';

afterEach(() => {
  useBuildStore.setState({ hasNewBuild: false });
  vi.unstubAllGlobals();
});

describe('NewBuildBanner', () => {
  it('renders nothing without a new build', () => {
    const { container } = render(<NewBuildBanner />);
    expect(container).toBeEmptyDOMElement();
  });

  it('offers a reload that calls location.reload', () => {
    const reload = vi.fn();
    vi.stubGlobal('location', { ...window.location, reload });
    useBuildStore.setState({ hasNewBuild: true });
    render(<NewBuildBanner />);
    fireEvent.click(screen.getByRole('button'));
    expect(reload).toHaveBeenCalledOnce();
  });
});
