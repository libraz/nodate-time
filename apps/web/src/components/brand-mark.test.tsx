import { cleanup, render, screen } from '@testing-library/react';
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

import { BrandMark, BrandRow } from './brand-mark';

afterEach(() => {
  cleanup();
  brand.logo = undefined;
});

describe('BrandMark', () => {
  it('draws the large tile with its halo', () => {
    const { container } = render(<BrandMark size="lg" />);
    expect(container.querySelector('[data-brand-mark="lg"]')).not.toBeNull();
    expect(container.querySelector('.auth-brand-mark')).not.toBeNull();
    expect(container.querySelector('.auth-halo')).not.toBeNull();
  });

  it('draws the small tile without the halo or the auth styling', () => {
    const { container } = render(<BrandMark size="sm" />);
    expect(container.querySelector('[data-brand-mark="sm"]')).not.toBeNull();
    expect(container.querySelector('.auth-brand-mark')).toBeNull();
    expect(container.querySelector('.auth-halo')).toBeNull();
  });
});

describe('BrandRow', () => {
  it('shows the name and the small tile by default', () => {
    const { container } = render(<BrandRow />);
    expect(screen.getByText('Acme')).toBeInTheDocument();
    expect(container.querySelector('[data-brand-mark="sm"]')).not.toBeNull();
    expect(container.querySelector('img')).toBeNull();
  });

  it('shows the logo instead of the tile when one is configured', () => {
    brand.logo = 'https://cdn.example.com/logo.svg';
    const { container } = render(<BrandRow />);
    expect(screen.getByText('Acme')).toBeInTheDocument();
    expect(container.querySelector('img')?.getAttribute('src')).toBe(brand.logo);
    expect(container.querySelector('[data-brand-mark]')).toBeNull();
  });
});
