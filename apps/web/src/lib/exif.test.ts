import { describe, expect, it } from 'vitest';
import { readCaptureTime, readCaptureTimeFromBuffer } from './exif';

interface AsciiTag {
  tag: number;
  value: string;
}

/**
 * Builds the smallest JPEG that carries the tags under test.
 *
 * A fixture file would say less: the layout is the thing being parsed, so
 * writing it out here is what makes a failure legible -- byte order, the
 * sub-IFD pointer, and values stored outside the entry that points at them.
 */
function jpegWithExif(opts: { littleEndian?: boolean; ifd0?: AsciiTag[]; exif?: AsciiTag[] }) {
  const le = opts.littleEndian ?? true;
  const ifd0 = opts.ifd0 ?? [];
  const exif = opts.exif ?? [];
  const hasExif = exif.length > 0;

  const ifd0Count = ifd0.length + (hasExif ? 1 : 0);
  const exifStart = 8 + 2 + ifd0Count * 12 + 4;
  const dataStart = hasExif ? exifStart + 2 + exif.length * 12 + 4 : exifStart;

  const asciiLength = (t: AsciiTag) => t.value.length + 1; // trailing NUL
  const total = dataStart + [...ifd0, ...exif].reduce((n, t) => n + asciiLength(t), 0);

  const tiff = new ArrayBuffer(total);
  const view = new DataView(tiff);
  view.setUint16(0, le ? 0x4949 : 0x4d4d);
  view.setUint16(2, 42, le);
  view.setUint32(4, 8, le);

  let dataAt = dataStart;
  const writeAscii = (entry: number, t: AsciiTag) => {
    const count = asciiLength(t);
    if (count <= 4) throw new Error('inline ASCII values are not exercised here');
    view.setUint16(entry, t.tag, le);
    view.setUint16(entry + 2, 2, le); // ASCII
    view.setUint32(entry + 4, count, le);
    view.setUint32(entry + 8, dataAt, le);
    for (let i = 0; i < t.value.length; i++) view.setUint8(dataAt + i, t.value.charCodeAt(i));
    view.setUint8(dataAt + t.value.length, 0);
    dataAt += count;
  };

  view.setUint16(8, ifd0Count, le);
  let entry = 10;
  for (const t of ifd0) {
    writeAscii(entry, t);
    entry += 12;
  }
  if (hasExif) {
    view.setUint16(entry, 0x8769, le); // ExifIFDPointer
    view.setUint16(entry + 2, 4, le); // LONG
    view.setUint32(entry + 4, 1, le);
    view.setUint32(entry + 8, exifStart, le);
    entry += 12;
  }
  view.setUint32(entry, 0, le); // no IFD1

  if (hasExif) {
    view.setUint16(exifStart, exif.length, le);
    let sub = exifStart + 2;
    for (const t of exif) {
      writeAscii(sub, t);
      sub += 12;
    }
    view.setUint32(sub, 0, le);
  }

  const app1Length = 2 + 6 + tiff.byteLength;
  const out = new Uint8Array(2 + 2 + app1Length + 2);
  const outView = new DataView(out.buffer);
  outView.setUint16(0, 0xffd8); // SOI
  outView.setUint16(2, 0xffe1); // APP1
  outView.setUint16(4, app1Length);
  out.set(new TextEncoder().encode('Exif'), 6);
  out[10] = 0;
  out[11] = 0;
  out.set(new Uint8Array(tiff), 12);
  outView.setUint16(out.byteLength - 2, 0xffd9); // EOI
  return out.buffer;
}

describe('readCaptureTimeFromBuffer', () => {
  it('reads the shutter time out of a little-endian JPEG', () => {
    const buffer = jpegWithExif({
      exif: [{ tag: 0x9003, value: '2026:03:08 05:30:00' }],
    });

    const taken = readCaptureTimeFromBuffer(buffer);
    // No offset tag, so the camera's wall clock is read as the reader's own --
    // which is what puts the photo beside the thing it was taken at.
    expect(taken?.getFullYear()).toBe(2026);
    expect(taken?.getMonth()).toBe(2);
    expect(taken?.getDate()).toBe(8);
    expect(taken?.getHours()).toBe(5);
    expect(taken?.getMinutes()).toBe(30);
  });

  // Both byte orders are in the wild; a parser that reads only one produces
  // plausible garbage on the other rather than failing.
  it('reads a big-endian JPEG the same way', () => {
    const buffer = jpegWithExif({
      littleEndian: false,
      exif: [{ tag: 0x9003, value: '2026:03:08 05:30:00' }],
    });

    const taken = readCaptureTimeFromBuffer(buffer);
    expect(taken?.getFullYear()).toBe(2026);
    expect(taken?.getHours()).toBe(5);
  });

  it('uses the recorded UTC offset when the camera wrote one', () => {
    const buffer = jpegWithExif({
      exif: [
        { tag: 0x9003, value: '2026:03:08 05:30:00' },
        { tag: 0x9011, value: '+09:00' },
      ],
    });

    // 05:30+09:00 is 20:30 UTC the day before, whatever zone the test runs in.
    expect(readCaptureTimeFromBuffer(buffer)?.toISOString()).toBe('2026-03-07T20:30:00.000Z');
  });

  it('falls back to the file timestamp when the shutter time is absent', () => {
    const buffer = jpegWithExif({
      ifd0: [{ tag: 0x0132, value: '2026:01:02 09:00:00' }],
      exif: [{ tag: 0x9011, value: '+00:00' }],
    });

    expect(readCaptureTimeFromBuffer(buffer)?.toISOString()).toBe('2026-01-02T09:00:00.000Z');
  });

  // A screenshot, a download, anything through an editor that strips metadata.
  // The album has to accept these, so "no capture time" must be an answer and
  // not a failure.
  it('reports nothing for a JPEG with no EXIF at all', () => {
    const bare = new Uint8Array([0xff, 0xd8, 0xff, 0xd9]);
    expect(readCaptureTimeFromBuffer(bare.buffer)).toBeNull();
  });

  it('reports nothing for a file that is not a JPEG', () => {
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
    expect(readCaptureTimeFromBuffer(png.buffer)).toBeNull();
  });

  it('reports nothing rather than throwing on a truncated file', () => {
    const full = new Uint8Array(
      jpegWithExif({ exif: [{ tag: 0x9003, value: '2026:03:08 05:30:00' }] }),
    );
    expect(() => readCaptureTimeFromBuffer(full.slice(0, 20).buffer)).not.toThrow();
    expect(readCaptureTimeFromBuffer(full.slice(0, 20).buffer)).toBeNull();
  });
});

describe('readCaptureTime', () => {
  it('reads a picked file', async () => {
    const buffer = jpegWithExif({
      exif: [
        { tag: 0x9003, value: '2026:07:04 12:00:00' },
        { tag: 0x9011, value: '+00:00' },
      ],
    });
    const file = new File([buffer], 'photo.jpg', { type: 'image/jpeg' });

    expect((await readCaptureTime(file))?.toISOString()).toBe('2026-07-04T12:00:00.000Z');
  });
});
