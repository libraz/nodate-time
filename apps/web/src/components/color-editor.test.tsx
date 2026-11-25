import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { COLOR_SWATCHES } from '@/types/calendar';

vi.mock('@/i18n', () => ({
  useT: () => (key: string) => key,
}));

import { ColorEditor } from './color-editor';

afterEach(cleanup);

function setup(value: string) {
  const onChange = vi.fn();
  render(
    <ColorEditor value={value} onChange={onChange} inUse={[]} legend="legend" inUseHint="hint" />,
  );
  return onChange;
}

describe('ColorEditor', () => {
  it('offers every swatch and keeps the custom section closed until asked', () => {
    setup('#E73B3B');

    for (const c of COLOR_SWATCHES) expect(screen.getByRole('button', { name: c })).toBeVisible();
    const toggle = screen.getByRole('button', { name: 'members.colorCustom' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('slider')).toBeNull();
    expect(screen.queryByLabelText('members.colorCode')).toBeNull();

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getAllByRole('slider')).toHaveLength(2);
    expect(screen.getByLabelText('members.colorCode')).toHaveValue('#E73B3B');
  });

  it('names a colour off the palette on the closed custom row', () => {
    setup('#123456');

    expect(screen.getByRole('button', { name: /members\.colorCustom/ })).toHaveTextContent(
      '#123456',
    );
    expect(screen.queryByRole('button', { pressed: true })).toBeNull();
  });

  it('commits a swatch in one click', () => {
    const onChange = setup('#E73B3B');

    fireEvent.click(screen.getByRole('button', { name: '#26B5B0' }));

    expect(onChange).toHaveBeenCalledWith('#26B5B0');
  });
});
