import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const brand = vi.hoisted(() => ({
  logo: undefined as { src: string; darkSrc: string | undefined } | undefined,
  icon: undefined as { src: string; darkSrc: string | undefined } | undefined,
}));

vi.mock('@/lib/brand', () => ({
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  APP_NAME: 'Acme',
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  get APP_LOGO() {
    return brand.logo;
  },
  // biome-ignore lint/style/useNamingConvention: must mirror the real module's exports
  get APP_ICON() {
    return brand.icon;
  },
}));

import { BrandHero, BrandImage, BrandMark, BrandRow } from './brand-mark';

afterEach(() => {
  cleanup();
  brand.logo = undefined;
  brand.icon = undefined;
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

const LOGO = { src: 'https://cdn.example.com/logo.svg', darkSrc: undefined };
const ICON = { src: 'https://cdn.example.com/icon.svg', darkSrc: undefined };

describe('BrandImage', () => {
  it('draws one image without a dark variant', () => {
    const { container } = render(<BrandImage asset={LOGO} alt="Acme" className="h-7" />);
    expect(container.querySelectorAll('img')).toHaveLength(1);
    expect(container.querySelector('span')?.className).toContain('h-7');
  });

  it('draws a light and a dark image with the same alt', () => {
    const { container } = render(
      <BrandImage asset={{ src: 'l.svg', darkSrc: 'd.svg' }} alt="Acme" />,
    );
    const imgs = container.querySelectorAll('img');
    expect(imgs).toHaveLength(2);
    expect(imgs[0]?.getAttribute('alt')).toBe('Acme');
    expect(imgs[1]?.getAttribute('alt')).toBe('Acme');
    expect(imgs[0]?.className).toContain('[[data-mode=dark]_&]:hidden');
    expect(imgs[1]?.className).toContain('[[data-mode=dark]_&]:block');
  });
});

describe('BrandRow', () => {
  it('shows the name and the small tile by default', () => {
    const { container } = render(<BrandRow />);
    expect(screen.getByText('Acme')).toBeInTheDocument();
    expect(container.querySelector('[data-brand-mark="sm"]')).not.toBeNull();
    expect(container.querySelector('img')).toBeNull();
  });

  it('shows only the logo, named by its alt, when one is configured', () => {
    brand.logo = LOGO;
    const { container } = render(<BrandRow />);
    expect(screen.queryByText('Acme')).toBeNull();
    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe(LOGO.src);
    expect(img?.getAttribute('alt')).toBe('Acme');
    expect(container.querySelector('[title]')).toBeNull();
    expect(container.querySelector('[data-brand-mark]')).toBeNull();
  });

  it('shows the icon and the name when only an icon is configured', () => {
    brand.icon = ICON;
    const { container } = render(<BrandRow />);
    expect(screen.getByText('Acme')).toHaveAttribute('title', 'Acme');
    expect(container.querySelector('img')?.getAttribute('src')).toBe(ICON.src);
    expect(container.querySelector('img')?.getAttribute('alt')).toBe('');
    expect(container.querySelector('[data-brand-mark]')).toBeNull();
  });
});

describe('BrandHero', () => {
  it('draws the large tile by default', () => {
    const { container } = render(<BrandHero alt="" />);
    expect(container.querySelector('[data-brand-mark="lg"]')).not.toBeNull();
    expect(container.querySelector('img')).toBeNull();
  });

  it('draws the logo with the given alt', () => {
    brand.logo = LOGO;
    const { container } = render(<BrandHero alt="Acme" />);
    expect(container.querySelector('img')?.getAttribute('alt')).toBe('Acme');
    expect(container.querySelector('span')?.className).toContain('max-w-[15rem]');
    expect(container.querySelector('[data-brand-mark]')).toBeNull();
  });

  it('draws the icon with an empty alt', () => {
    brand.icon = ICON;
    const { container } = render(<BrandHero alt="Acme" />);
    expect(container.querySelector('img')?.getAttribute('src')).toBe(ICON.src);
    expect(container.querySelector('img')?.getAttribute('alt')).toBe('');
    expect(container.querySelector('span')?.className).toContain('w-16');
  });
});
