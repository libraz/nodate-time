import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { Avatar } from './avatar';

afterEach(cleanup);

describe('Avatar', () => {
  // The server answers with the picture as a URL, and the failure this guards
  // against is not a missing avatar but a visible one: a string like
  // "https://storage/..." printed into the row where the face belongs.
  it('shows a picture as an image rather than printing its URL', () => {
    const url = 'https://storage.example/avatars/u1?signature=abc';
    const { container } = render(<Avatar src={url} name="Hanako" />);

    const img = container.querySelector('img');
    expect(img).not.toBeNull();
    expect(img?.getAttribute('src')).toBe(url);
    expect(screen.queryByText(url)).toBeNull();
  });

  it('stands in with the initial when there is no picture', () => {
    const { container } = render(<Avatar name="Hanako" />);

    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByText('H')).toBeInTheDocument();
  });
});
