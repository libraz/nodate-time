/**
 * Minimal EXIF reader for the one thing the album needs: when a photo was
 * taken.
 *
 * The album orders by capture time, not upload time, so that photos of an
 * occasion sit together however long afterwards somebody gets round to
 * uploading them. Without this the order is whatever sequence the files were
 * dropped in, which for a shared album is nobody's order at all.
 *
 * Only the tags required for that are parsed. A general EXIF library would
 * read hundreds more and none of them would be used.
 */

/** APP1 is the JPEG marker segment EXIF lives in. */
const app1Marker = 0xffe1;
/** Tag 0x9003, the time the shutter fired. */
const tagDateTimeOriginal = 0x9003;
/** Tag 0x9011, that time's UTC offset, present only on newer cameras. */
const tagOffsetTimeOriginal = 0x9011;
/** Tag 0x0132, the file's own timestamp -- a fallback, since editors rewrite it. */
const tagDateTime = 0x0132;
/** Tag 0x8769, the offset of the sub-IFD the exposure tags live in. */
const tagExifIfdPointer = 0x8769;

const typeAscii = 2;
const typeLong = 4;

interface Entry {
  type: number;
  valueOffset: number;
  count: number;
}

interface Ifd {
  view: DataView;
  /** Offset of the TIFF header, which every pointer inside EXIF is relative to. */
  tiffStart: number;
  littleEndian: boolean;
}

function readEntries(ifd: Ifd, ifdOffset: number): Map<number, Entry> {
  const out = new Map<number, Entry>();
  const base = ifd.tiffStart + ifdOffset;
  if (base + 2 > ifd.view.byteLength) return out;
  const count = ifd.view.getUint16(base, ifd.littleEndian);
  for (let i = 0; i < count; i++) {
    const entry = base + 2 + i * 12;
    if (entry + 12 > ifd.view.byteLength) break;
    const tag = ifd.view.getUint16(entry, ifd.littleEndian);
    const type = ifd.view.getUint16(entry + 2, ifd.littleEndian);
    const valueCount = ifd.view.getUint32(entry + 4, ifd.littleEndian);
    // A value of four bytes or fewer is stored in the entry itself; anything
    // longer is stored elsewhere and the entry holds its offset.
    const inlineSize = type === typeAscii ? valueCount : 4;
    const valueOffset =
      inlineSize <= 4 ? entry + 8 : ifd.tiffStart + ifd.view.getUint32(entry + 8, ifd.littleEndian);
    out.set(tag, { type, valueOffset, count: valueCount });
  }
  return out;
}

function readAscii(ifd: Ifd, entry: Entry): string {
  const end = Math.min(entry.valueOffset + entry.count, ifd.view.byteLength);
  let out = '';
  for (let i = entry.valueOffset; i < end; i++) {
    const byte = ifd.view.getUint8(i);
    if (byte === 0) break;
    out += String.fromCharCode(byte);
  }
  return out;
}

/** Locates the TIFF header inside a JPEG's APP1 segment, if there is one. */
function findTiffStart(view: DataView): number | null {
  if (view.byteLength < 4 || view.getUint16(0) !== 0xffd8) return null; // not a JPEG
  let offset = 2;
  while (offset + 4 <= view.byteLength) {
    const marker = view.getUint16(offset);
    // Every segment marker starts 0xFF. Anything else means the scan data has
    // begun and there is no metadata left to find.
    if ((marker & 0xff00) !== 0xff00) return null;
    const length = view.getUint16(offset + 2);
    if (marker === app1Marker) {
      const header = offset + 4;
      // "Exif\0\0" -- APP1 also carries XMP, which is a different payload.
      if (
        header + 6 <= view.byteLength &&
        view.getUint32(header) === 0x45786966 &&
        view.getUint16(header + 4) === 0
      ) {
        return header + 6;
      }
    }
    if (length < 2) return null;
    offset += 2 + length;
  }
  return null;
}

const exifDatePattern = /^(\d{4}):(\d{2}):(\d{2})[ T](\d{2}):(\d{2}):(\d{2})/;
const utcOffsetPattern = /^[+-]\d{2}:\d{2}$/;

/**
 * Parses "YYYY:MM:DD HH:MM:SS" plus an optional "+09:00" offset.
 *
 * EXIF records the time on the camera's own clock with no zone attached, so
 * without an offset tag the only reading available is the one the person
 * looking at the photo would give it: their own wall clock. That is also what
 * phone galleries do, and it is what puts a photo taken at noon next to the
 * lunch it was taken at.
 */
function parseExifDate(value: string, offset: string | null): Date | null {
  const parts = value.trim().match(exifDatePattern);
  if (!parts) return null;
  const [, year, month, day, hour, minute, second] = parts;
  const stamp = `${year}-${month}-${day}T${hour}:${minute}:${second}`;
  const trimmed = offset?.trim() ?? '';
  const zone = utcOffsetPattern.test(trimmed) ? trimmed : '';
  const parsed = new Date(stamp + zone);
  return Number.isNaN(parsed.getTime()) ? null : parsed;
}

/**
 * Reads the capture time out of a JPEG, or null when the file carries none --
 * which is the common case for screenshots, downloads, and anything that has
 * been through an editor that strips metadata.
 */
export function readCaptureTimeFromBuffer(buffer: ArrayBuffer): Date | null {
  const view = new DataView(buffer);
  const tiffStart = findTiffStart(view);
  if (tiffStart === null || tiffStart + 8 > view.byteLength) return null;

  const byteOrder = view.getUint16(tiffStart);
  if (byteOrder !== 0x4949 && byteOrder !== 0x4d4d) return null;
  const littleEndian = byteOrder === 0x4949;
  if (view.getUint16(tiffStart + 2, littleEndian) !== 42) return null;

  const ifd: Ifd = { view, tiffStart, littleEndian };
  const ifd0 = readEntries(ifd, view.getUint32(tiffStart + 4, littleEndian));

  const pointer = ifd0.get(tagExifIfdPointer);
  const exif: Map<number, Entry> =
    pointer && pointer.type === typeLong
      ? readEntries(ifd, view.getUint32(pointer.valueOffset, littleEndian))
      : new Map();

  const offsetEntry = exif.get(tagOffsetTimeOriginal);
  const offset = offsetEntry ? readAscii(ifd, offsetEntry) : null;

  const original = exif.get(tagDateTimeOriginal);
  if (original) {
    const parsed = parseExifDate(readAscii(ifd, original), offset);
    if (parsed) return parsed;
  }
  // Falls back to the file timestamp, which editing software rewrites -- so it
  // is worth less than the shutter time, and only worth more than nothing.
  const fallback = ifd0.get(tagDateTime);
  return fallback ? parseExifDate(readAscii(ifd, fallback), offset) : null;
}

/**
 * readCaptureTimeFromBuffer for a picked file. Never throws: a photo whose
 * metadata cannot be read is still a photo worth uploading.
 */
export async function readCaptureTime(file: File): Promise<Date | null> {
  try {
    return readCaptureTimeFromBuffer(await file.arrayBuffer());
  } catch {
    return null;
  }
}
