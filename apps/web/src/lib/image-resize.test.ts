import { describe, expect, it } from 'vitest';
import {
  albumMaxDimension,
  albumThumbnailMaxDimension,
  planAlbumEncoding,
  planAlbumThumbnail,
} from './image-resize';

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

/**
 * The grid draws tiles of about 134px from the stored photo, which is up to
 * 2048px on its longest edge -- megabytes to show a screenful of thumbnails.
 * A second, small image is generated at upload time so the grid has something
 * its own size to load.
 */
describe('planAlbumThumbnail', () => {
  it('gives an animated GIF no thumbnail at all', () => {
    // Canvas draws one frame, so the thumbnail would be a still where the grid
    // animates today. Falling back to the photo keeps the animation.
    expect(planAlbumThumbnail('image/gif', 2048)).toEqual({ thumbnail: false });
  });

  it('keeps a PNG thumbnail a PNG', () => {
    // A JPEG thumbnail of a transparent PNG draws the transparency black, in
    // the grid, where every photo is on screen at once.
    expect(planAlbumThumbnail('image/png', 2048)).toEqual({
      thumbnail: true,
      type: 'image/png',
    });
  });

  it('keeps a WebP thumbnail WebP', () => {
    expect(planAlbumThumbnail('image/webp', 2048)).toEqual({
      thumbnail: true,
      type: 'image/webp',
    });
  });

  it('makes a JPEG thumbnail of a JPEG', () => {
    expect(planAlbumThumbnail('image/jpeg', 2048)).toEqual({
      thumbnail: true,
      type: 'image/jpeg',
    });
  });

  it('skips a photo already no larger than a thumbnail', () => {
    // Uploading a second copy of the same picture costs a request and saves
    // the grid nothing.
    expect(planAlbumThumbnail('image/jpeg', albumThumbnailMaxDimension)).toEqual({
      thumbnail: false,
    });
  });

  it('makes one for a photo a single pixel over', () => {
    expect(planAlbumThumbnail('image/jpeg', albumThumbnailMaxDimension + 1)).toEqual({
      thumbnail: true,
      type: 'image/jpeg',
    });
  });

  it('is not fooled by an uppercase content type', () => {
    expect(planAlbumThumbnail('IMAGE/GIF', 2048)).toEqual({ thumbnail: false });
  });
});
