import { describe, expect, it, vi } from 'vitest';
import { ApiError } from './api';
import { sha256Hex, validateUpload } from './upload';

/** The rejection `validateUpload` threw, so the assertions can read its status
 *  rather than a message that changes with the interface language. */
function rejectionOf(
  kind: 'avatar' | 'album' | 'attachment',
  type: string,
  size: number,
): ApiError {
  try {
    validateUpload(kind, type, size);
  } catch (e) {
    if (e instanceof ApiError) return e;
    throw e;
  }
  throw new Error(`expected ${type} at ${size} bytes to be rejected`);
}

describe('validateUpload', () => {
  // The album handler checks an exact allowlist, so a prefix rule here let
  // image/svg+xml -- markup a browser executes, not a picture -- past the file
  // picker and into a presign the server then refuses.
  it('refuses SVG for an album photo, as the server does', () => {
    expect(rejectionOf('album', 'image/svg+xml', 1024).status).toBe(415);
  });

  it('refuses SVG for an attachment, which otherwise takes any type', () => {
    expect(() => validateUpload('attachment', 'application/pdf', 1024)).not.toThrow();
    expect(rejectionOf('attachment', 'image/svg+xml', 1024).status).toBe(415);
  });

  // The server reads the media type with the parameters stripped, so a client
  // matching the whole header string would judge these two differently.
  it('ignores parameters and case when reading the media type', () => {
    expect(rejectionOf('album', 'image/SVG+XML; charset=utf-8', 1024).status).toBe(415);
    expect(() => validateUpload('album', 'IMAGE/JPEG; charset=binary', 1024)).not.toThrow();
  });

  it('accepts the formats the album endpoint stores', () => {
    for (const type of ['image/jpeg', 'image/png', 'image/webp', 'image/gif']) {
      expect(() => validateUpload('album', type, 1024)).not.toThrow();
    }
  });

  it('refuses a picture format the avatar endpoint does not take', () => {
    expect(rejectionOf('avatar', 'image/gif', 1024).status).toBe(415);
  });

  it('refuses an empty file, which no upload URL can be signed for', () => {
    expect(rejectionOf('attachment', 'application/pdf', 0).status).toBe(400);
  });

  it('refuses a file past the limit for its kind', () => {
    expect(rejectionOf('avatar', 'image/png', 6 * 1024 * 1024).status).toBe(413);
    expect(rejectionOf('attachment', 'application/pdf', 101 * 1024 * 1024).status).toBe(413);
  });
});

describe('sha256Hex', () => {
  const subtleDigest = async (bytes: Uint8Array<ArrayBuffer>): Promise<string> => {
    const digest = await crypto.subtle.digest('SHA-256', bytes);
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('');
  };

  it('matches the published digest of a known string', async () => {
    expect(await sha256Hex(new TextEncoder().encode('abc').buffer as ArrayBuffer)).toBe(
      'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
    );
  });

  it('digests an empty input', async () => {
    expect(await sha256Hex(new Blob([]))).toBe(
      'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
    );
  });

  // An attachment may be 100MB, and asking a Blob for all of its bytes is
  // what makes that 100MB of memory: until then a File is backed by disk.
  it('reads a file in slices rather than all at once', async () => {
    const blob = new Blob([new Uint8Array(9 * 1024 * 1024)]);
    const takeSlice = blob.slice.bind(blob);
    const sliceSizes: number[] = [];
    vi.spyOn(blob, 'slice').mockImplementation((start?: number, end?: number) => {
      const part = takeSlice(start, end);
      sliceSizes.push(part.size);
      return part;
    });
    const readWhole = vi.spyOn(blob, 'arrayBuffer');

    await sha256Hex(blob);

    expect(readWhole).not.toHaveBeenCalled();
    expect(Math.max(...sliceSizes)).toBeLessThanOrEqual(4 * 1024 * 1024);
  });

  // A Blob is read a slice at a time so a large file is never resident whole,
  // which means the incremental path has to agree with the one-shot one at
  // every length -- especially around a block and a slice boundary.
  it('agrees with crypto.subtle across block and slice boundaries', async () => {
    for (const size of [1, 55, 56, 63, 64, 65, 127, 128, 1000, 4 * 1024 * 1024 + 3]) {
      const bytes = new Uint8Array(size);
      for (let i = 0; i < size; i++) bytes[i] = (i * 31 + 7) % 256;
      expect(await sha256Hex(new Blob([bytes]))).toBe(await subtleDigest(bytes));
    }
  });
});
