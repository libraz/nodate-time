import { describe, expect, it } from 'vitest';
import { albumMaxDimension, planAlbumEncoding } from './image-resize';

/**
 * Every album upload used to be re-encoded to JPEG regardless of what was
 * picked. What that costs is invisible at upload time and permanent
 * afterwards, so these fix each of the three losses separately.
 */
describe('planAlbumEncoding', () => {
  it('leaves an animated GIF alone even when it is oversized', () => {
    // Canvas draws one frame. Re-encoding turns an animation into a still, and
    // nothing in the file says whether it was animated, so the only safe
    // reading is that it might have been.
    expect(planAlbumEncoding('image/gif', 4000)).toEqual({ passthrough: true });
  });

  it('keeps a PNG a PNG when it has to be resized', () => {
    // JPEG has no alpha channel: transparency comes back as black.
    expect(planAlbumEncoding('image/png', 4000)).toEqual({
      passthrough: false,
      type: 'image/png',
    });
  });

  it('sends a photo that is already small enough untouched', () => {
    // Re-encoding it would strip the EXIF block -- and with it the capture
    // time the album orders by -- in exchange for nothing.
    expect(planAlbumEncoding('image/jpeg', albumMaxDimension)).toEqual({ passthrough: true });
  });

  it('resizes a photo larger than the stored dimension', () => {
    expect(planAlbumEncoding('image/jpeg', albumMaxDimension + 1)).toEqual({
      passthrough: false,
      type: 'image/jpeg',
    });
  });

  it('keeps WebP as WebP', () => {
    expect(planAlbumEncoding('image/webp', 4000)).toEqual({
      passthrough: false,
      type: 'image/webp',
    });
  });

  it('re-encodes a type the album does not store, however small', () => {
    // A HEIC from an iPhone, say: the server's allow-list would refuse it, so
    // passing the original through would fail the upload instead of resizing.
    expect(planAlbumEncoding('image/heic', 100)).toEqual({
      passthrough: false,
      type: 'image/jpeg',
    });
  });

  it('is not fooled by an uppercase content type', () => {
    expect(planAlbumEncoding('IMAGE/PNG', 4000)).toEqual({
      passthrough: false,
      type: 'image/png',
    });
  });
});
