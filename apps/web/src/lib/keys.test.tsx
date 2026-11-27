import { fireEvent, render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { isSubmitEnter, isSubmitShortcut } from './keys';

function seen(init: KeyboardEventInit & { keyCode?: number }): boolean {
  let result = false;
  const { container } = render(
    <input
      onKeyDown={(e) => {
        result = isSubmitEnter(e);
      }}
    />,
  );
  fireEvent.keyDown(container.querySelector('input') as HTMLInputElement, init);
  return result;
}

describe('isSubmitEnter', () => {
  it('takes a plain Enter', () => {
    expect(seen({ key: 'Enter' })).toBe(true);
  });

  it('leaves the Enter that commits an IME conversion', () => {
    expect(seen({ key: 'Enter', isComposing: true })).toBe(false);
    expect(seen({ key: 'Enter', keyCode: 229 })).toBe(false);
  });

  it('ignores other keys', () => {
    expect(seen({ key: 'a' })).toBe(false);
  });
});

describe('isSubmitShortcut', () => {
  function shortcut(init: KeyboardEventInit & { keyCode?: number }): boolean {
    let result = false;
    const { container } = render(
      <input
        onKeyDown={(e) => {
          result = isSubmitShortcut(e);
        }}
      />,
    );
    fireEvent.keyDown(container.querySelector('input') as HTMLInputElement, init);
    return result;
  }

  it('takes Enter with Cmd or Ctrl', () => {
    expect(shortcut({ key: 'Enter', metaKey: true })).toBe(true);
    expect(shortcut({ key: 'Enter', ctrlKey: true })).toBe(true);
  });

  it('leaves a plain Enter and other keys', () => {
    expect(shortcut({ key: 'Enter' })).toBe(false);
    expect(shortcut({ key: 'a', ctrlKey: true })).toBe(false);
  });

  it('leaves the Enter that commits an IME conversion', () => {
    expect(shortcut({ key: 'Enter', ctrlKey: true, isComposing: true })).toBe(false);
    expect(shortcut({ key: 'Enter', metaKey: true, keyCode: 229 })).toBe(false);
  });
});
