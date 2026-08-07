export interface ResizedImage {
  bytes: ArrayBuffer;
  contentType: string;
  width: number;
  height: number;
}

/** The encodings canvas can be asked for without losing the image entirely. */
type EncodableType = 'image/jpeg' | 'image/png' | 'image/webp';

interface ResizeOptions {
  maxDimension: number;
  quality?: number;
  preferredType?: EncodableType;
}

async function readAsImage(file: File): Promise<HTMLImageElement> {
  const url = URL.createObjectURL(file);
  try {
    return await new Promise<HTMLImageElement>((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve(img);
      img.onerror = () => reject(new Error('failed to decode image'));
      img.src = url;
    });
  } finally {
    URL.revokeObjectURL(url);
  }
}

async function canvasToBlob(
  canvas: HTMLCanvasElement,
  type: string,
  quality: number,
): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (blob) => {
        if (blob) resolve(blob);
        else reject(new Error('canvas.toBlob returned null'));
      },
      type,
      quality,
    );
  });
}

async function resize(file: File, opts: ResizeOptions): Promise<ResizedImage> {
  const img = await readAsImage(file);
  const longest = Math.max(img.naturalWidth, img.naturalHeight);
  const scale = longest > opts.maxDimension ? opts.maxDimension / longest : 1;
  const width = Math.round(img.naturalWidth * scale);
  const height = Math.round(img.naturalHeight * scale);

  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('2d context unavailable');
  ctx.drawImage(img, 0, 0, width, height);

  const type = opts.preferredType ?? 'image/jpeg';
  const blob = await canvasToBlob(canvas, type, opts.quality ?? 0.9);
  const bytes = await blob.arrayBuffer();
  return { bytes, contentType: type, width, height };
}

export function resizeImageForAvatar(file: File): Promise<ResizedImage> {
  return resize(file, { maxDimension: 512, quality: 0.92, preferredType: 'image/jpeg' });
}

/** The longest edge an album photo is stored at. */
export const albumMaxDimension = 2048;

/** The types the album accepts, matching the server's own allow-list. */
const albumTypes = new Set(['image/jpeg', 'image/png', 'image/webp', 'image/gif']);

/**
 * What should be uploaded for a picked file: either its own bytes, or a
 * re-encoding in a named format.
 */
export type EncodingPlan = { passthrough: true } | { passthrough: false; type: EncodableType };

/**
 * Decides how a picked photo should be sent.
 *
 * Re-encoding everything to JPEG -- which is what this used to do
 * unconditionally -- costs three things that cannot be recovered afterwards: a
 * PNG's transparency becomes black, an animated GIF becomes its first frame,
 * and the EXIF block goes altogether, taking the capture time the album orders
 * by with it. None of that is visible at upload time; it is discovered later,
 * by whoever opens the photo.
 *
 * So the file is left alone unless resizing it actually buys something, and
 * when it does have to be re-encoded it keeps its own format.
 */
export function planAlbumEncoding(
  sourceType: string,
  longestEdge: number,
  maxDimension = albumMaxDimension,
): EncodingPlan {
  const type = sourceType.toLowerCase();

  // A GIF is never re-encoded. Canvas draws one frame, so re-encoding an
  // animated GIF silently discards every other frame -- and whether a GIF is
  // animated cannot be told without decoding it, so the safe reading is that
  // it might be.
  if (type === 'image/gif') return { passthrough: true };

  // Already small enough and in a format the album stores: the original bytes
  // are strictly better than anything a re-encode would produce.
  if (albumTypes.has(type) && longestEdge <= maxDimension) return { passthrough: true };

  if (type === 'image/png') return { passthrough: false, type: 'image/png' };
  if (type === 'image/webp') return { passthrough: false, type: 'image/webp' };
  return { passthrough: false, type: 'image/jpeg' };
}

/**
 * Prepares a picked photo for upload, resizing only when that is what the file
 * needs. See planAlbumEncoding for why the format is preserved.
 */
export async function prepareImageForAlbum(file: File): Promise<ResizedImage> {
  const img = await readAsImage(file);
  const longest = Math.max(img.naturalWidth, img.naturalHeight);
  const plan = planAlbumEncoding(file.type, longest);

  if (plan.passthrough) {
    return {
      bytes: await file.arrayBuffer(),
      contentType: file.type,
      width: img.naturalWidth,
      height: img.naturalHeight,
    };
  }
  return resize(file, {
    maxDimension: albumMaxDimension,
    quality: 0.88,
    preferredType: plan.type,
  });
}
