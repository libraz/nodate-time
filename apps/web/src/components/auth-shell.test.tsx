import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const brand = vi.hoisted(() => ({ logo: undefined as string | undefined }));

vi.mock('@/lib/brand', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_NAME: 'Acme',
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  get APP_LOGO_URL() {
    return brand.logo;
  },
}));

import { AuthShell } from './auth-shell';

afterEach(() => {
  cleanup();
  brand.logo = undefined;
});

describe('AuthShell brand mark', () => {
  it('draws the logo instead of the tile when one is configured', () => {
    brand.logo = 'https://cdn.example.com/logo.svg';
    const { container } = render(<AuthShell title="Acme">body</AuthShell>);
    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe('https://cdn.example.com/logo.svg');
    expect(img?.getAttribute('alt')).toBe('');
    expect(container.querySelector('.auth-brand-mark')).toBeNull();
    expect(container.querySelector('.auth-halo')).toBeNull();
  });

  it('keeps the tile and draws no image without a logo', () => {
    const { container } = render(<AuthShell title="Acme">body</AuthShell>);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('.auth-brand-mark')).not.toBeNull();
    expect(container.querySelector('.auth-halo')).not.toBeNull();
  });
});
