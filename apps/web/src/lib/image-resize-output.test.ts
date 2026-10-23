import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  prepareAlbumThumbnail,
  prepareImageForAlbum,
  type ResizedImage,
  resizeImageForAvatar,
} from './image-resize';

interface ImageDimensions {
  width: number;
  height: number;
  decode?: boolean;
}

interface CanvasRecord {
  canvas: HTMLCanvasElement;
  requestedType: string;
}

let dimensions: ImageDimensions;
let encodedType: string | undefined;
let canvases: CanvasRecord[];
let createObjectURL: ReturnType<typeof vi.fn>;
let revokeObjectURL: ReturnType<typeof vi.fn>;

class ControlledImage {
  naturalWidth = dimensions.width;
  naturalHeight = dimensions.height;
  onload: ((event: Event) => unknown) | null = null;
  onerror: ((event: Event) => unknown) | null = null;

  set src(_value: string) {
    queueMicrotask(() => {
      if (dimensions.decode === false) {
        this.onerror?.(new Event('error'));
      } else {
        this.onload?.(new Event('load'));
      }
    });
  }
}

class ControlledCanvas {
  width = 0;
  height = 0;

  getContext(): CanvasRenderingContext2D {
    return { drawImage: vi.fn() } as unknown as CanvasRenderingContext2D;
  }

  toBlob(callback: BlobCallback, type = ''): void {
    canvases.push({ canvas: this as unknown as HTMLCanvasElement, requestedType: type });
    callback(new Blob([new Uint8Array([1, 2, 3, 4])], { type: encodedType ?? type }));
  }
}

function imageFile(type: string): File {
  return {
    type,
    arrayBuffer: vi.fn(async () => new ArrayBuffer(4)),
  } as unknown as File;
}

function resized(bytes = 1000, width = 2048, height = 1024): ResizedImage {
  return {
    bytes: new ArrayBuffer(bytes),
    contentType: 'image/webp',
    width,
    height,
  };
}

beforeEach(() => {
  dimensions = { width: 3000, height: 1500 };
  encodedType = undefined;
  canvases = [];
  createObjectURL = vi.fn(() => 'blob:test-image');
  revokeObjectURL = vi.fn();

  vi.stubGlobal('Image', ControlledImage);
  vi.stubGlobal('URL', { createObjectURL, revokeObjectURL });
  vi.stubGlobal('document', {
    createElement: vi.fn((tagName: string) => {
      if (tagName !== 'canvas') throw new Error(`unexpected element: ${tagName}`);
      return new ControlledCanvas();
    }),
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('image resize output', () => {
  it.each([
    { width: 10_000, height: 1, expectedWidth: 512, expectedHeight: 1 },
    { width: 1, height: 10_000, expectedWidth: 1, expectedHeight: 512 },
  ])('keeps a resized $width-by-$height image nonzero', async (input) => {
    dimensions = { width: input.width, height: input.height };

    const result = await resizeImageForAvatar(imageFile('image/jpeg'));

    expect(result.width).toBe(input.expectedWidth);
    expect(result.height).toBe(input.expectedHeight);
    expect(result.width).toBeGreaterThan(0);
    expect(result.height).toBeGreaterThan(0);
    expect(Math.max(result.width, result.height)).toBeLessThanOrEqual(512);
  });

  it.each([
    { source: 'image/png', encoded: 'image/png' },
    { source: 'image/webp', encoded: 'image/webp' },
    { source: 'image/jpeg', encoded: 'image/jpeg' },
  ])('re-encodes a $source avatar as $encoded', async ({ source, encoded }) => {
    dimensions = { width: 1024, height: 1024 };

    const result = await resizeImageForAvatar(imageFile(source));

    expect(canvases[0]?.requestedType).toBe(encoded);
    expect(result.contentType).toBe(encoded);
  });

  it('keeps an already small album image at its original dimensions', async () => {
    dimensions = { width: 320, height: 240 };

    const result = await prepareImageForAlbum(imageFile('image/jpeg'));

    expect(result.width).toBe(320);
    expect(result.height).toBe(240);
    expect(canvases).toHaveLength(0);
  });

  it('decodes an oversized album image once before resizing it', async () => {
    dimensions = { width: 3000, height: 1500 };

    const result = await prepareImageForAlbum(imageFile('image/jpeg'));

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:test-image');
    expect(revokeObjectURL).toHaveBeenCalledTimes(1);
    expect(result.width).toBe(2048);
    expect(result.height).toBe(1024);
    expect(result.contentType).toBe('image/jpeg');
    expect(canvases).toHaveLength(1);
  });

  it('reports the encoder output type when a thumbnail falls back from WebP to PNG', async () => {
    encodedType = 'image/png';
    dimensions = { width: 3000, height: 1500 };

    const result = await prepareAlbumThumbnail(imageFile('image/webp'), resized());

    expect(result).not.toBeNull();
    expect(canvases[0]?.requestedType).toBe('image/webp');
    expect(result?.contentType).toBe('image/png');
  });

  it('preserves JPEG as the reported type when the encoder supports it', async () => {
    dimensions = { width: 3000, height: 1500 };

    const result = await prepareImageForAlbum(imageFile('image/jpeg'));

    expect(canvases.at(-1)?.requestedType).toBe('image/jpeg');
    expect(result.contentType).toBe('image/jpeg');
  });

  it('releases the object URL when image decoding fails', async () => {
    dimensions = { width: 3000, height: 1500, decode: false };

    await expect(resizeImageForAvatar(imageFile('image/jpeg'))).rejects.toThrow(
      'failed to decode image',
    );

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:test-image');
  });
});
