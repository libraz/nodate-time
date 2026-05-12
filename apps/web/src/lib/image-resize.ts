export interface ResizedImage {
  bytes: ArrayBuffer;
  contentType: string;
  width: number;
  height: number;
}

interface ResizeOptions {
  maxDimension: number;
  quality?: number;
  preferredType?: 'image/jpeg' | 'image/webp';
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

export function resizeImageForAlbum(file: File): Promise<ResizedImage> {
  return resize(file, { maxDimension: 2048, quality: 0.88, preferredType: 'image/jpeg' });
}
