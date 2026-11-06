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

import { AuthShell } from './auth-shell';

afterEach(() => {
  cleanup();
  brand.logo = undefined;
  brand.icon = undefined;
});

const LOGO = { src: 'https://cdn.example.com/logo.svg', darkSrc: undefined };

describe('AuthShell brand', () => {
  it('hides the heading and leaves the logo unnamed when the title is omitted', () => {
    brand.logo = LOGO;
    const { container } = render(<AuthShell>body</AuthShell>);
    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe(LOGO.src);
    expect(img?.getAttribute('alt')).toBe('');
    const heading = screen.getByRole('heading', { level: 1 });
    expect(heading).toHaveTextContent('Acme');
    expect(heading.className).toContain('sr-only');
    expect(container.querySelector('.auth-brand-mark')).toBeNull();
  });

  it('names the logo by the app when a title is given', () => {
    brand.logo = LOGO;
    const { container } = render(<AuthShell title="Reset">body</AuthShell>);
    expect(container.querySelector('img')?.getAttribute('alt')).toBe('Acme');
    const heading = screen.getByRole('heading', { level: 1 });
    expect(heading).toHaveTextContent('Reset');
    expect(heading.className).not.toContain('sr-only');
  });

  it('shows the app name as a visible heading without a logo', () => {
    const { container } = render(<AuthShell>body</AuthShell>);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('.auth-brand-mark')).not.toBeNull();
    expect(container.querySelector('.auth-halo')).not.toBeNull();
    expect(screen.getByRole('heading', { level: 1 }).className).not.toContain('sr-only');
  });
});
