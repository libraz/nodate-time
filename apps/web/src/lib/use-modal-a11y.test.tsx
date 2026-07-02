import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useModalA11y } from './use-modal-a11y';

function Dialog({ onClose }: { onClose: () => void }) {
  const ref = useModalA11y<HTMLDivElement>(true, onClose);
  return (
    <div ref={ref}>
      <button type="button">First</button>
      <button type="button">Last</button>
    </div>
  );
}

function rectList(): DOMRectList {
  const rects = [new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList;
  rects.item = (index: number) => rects[index] ?? null;
  return rects;
}

afterEach(() => {
  document.body.style.overflow = '';
});

describe('useModalA11y', () => {
  it('locks body scroll, closes on Escape, and traps Tab inside the container', () => {
    const onClose = vi.fn();
    render(<Dialog onClose={onClose} />);
    for (const button of screen.getAllByRole('button')) {
      vi.spyOn(button, 'getClientRects').mockReturnValue(rectList());
    }

    expect(document.body.style.overflow).toBe('hidden');

    const [first, last] = screen.getAllByRole('button');
    expect(first).toBeDefined();
    expect(last).toBeDefined();
    if (!first || !last) return;
    last.focus();
    fireEvent.keyDown(document, { key: 'Tab' });
    expect(document.activeElement).toBe(first);

    first.focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
