import { getT } from '@/i18n';
import { ApiError, client, present } from '@/lib/client';
import { Sha256, toHex } from '@/lib/sha256';
import type { Calendar } from '@/types/calendar';

/**
 * Server-side upload limits, mirrored on the client to fail fast before
 * presigning.
 *
 * `types` are exact media types, matching the allowlists the handlers check
 * against. A prefix rule such as "image/" would not: it admits image/svg+xml,
 * which the server refuses because a browser renders it with whatever script
 * it carries. Letting one through here only moves the rejection to after the
 * file was picked, which reads as the upload failing for no reason.
 */
export const UPLOAD_LIMITS = {
  avatar: { maxBytes: 5 * 1024 * 1024, types: ['image/jpeg', 'image/png', 'image/webp'] },
  album: {
    maxBytes: 20 * 1024 * 1024,
    types: ['image/jpeg', 'image/png', 'image/webp', 'image/gif'],
  },
  attachment: { maxBytes: 100 * 1024 * 1024, types: [] as string[] },
  cover: { maxBytes: 5 * 1024 * 1024, types: ['image/jpeg', 'image/png', 'image/webp'] },
} as const;

export type UploadKind = keyof typeof UPLOAD_LIMITS;

/**
 * Types no endpoint accepts, whatever else it allows. Attachments take any
 * media type, so an allowlist cannot express this one: SVG is turned away
 * because it is markup a browser will execute, not a picture.
 */
const REJECTED_TYPES: readonly string[] = ['image/svg+xml'];

/**
 * The media type alone, lowercased and without parameters — the same reading
 * the server takes, so "image/SVG+XML; charset=utf-8" is judged as SVG rather
 * than as an unrecognized string that no rule happens to match.
 */
function mediaType(contentType: string): string {
  const [base = ''] = contentType.split(';');
  return base.trim().toLowerCase();
}

/** Returns true when `contentType` is one the endpoint for `kind` accepts. */
function typeAllowed(contentType: string, allowed: readonly string[]): boolean {
  const type = mediaType(contentType);
  if (REJECTED_TYPES.includes(type)) return false;
  if (allowed.length === 0) return true;
  return allowed.includes(type);
}

/** Validates `byteSize`/`contentType` against the limits for `kind`, throwing a localized error. */
export function validateUpload(kind: UploadKind, contentType: string, byteSize: number): void {
  const t = getT();
  const limit = UPLOAD_LIMITS[kind];
  // The size is signed into the upload URL, so the server has no way to issue
  // one for zero bytes; refusing here names the reason instead.
  if (byteSize < 1) {
    throw new ApiError(400, t('error.emptyFile'));
  }
  if (byteSize > limit.maxBytes) {
    throw new ApiError(413, t('error.fileTooLarge'));
  }
  if (!typeAllowed(contentType, limit.types)) {
    throw new ApiError(415, t('error.unsupportedFileType'));
  }
}

interface PresignResult {
  /** Absent when the bytes are already stored and in use; the reservation then goes straight to confirm. */
  uploadUrl?: string | undefined;
}

/**
 * How much of a file is read at a time when digesting it. Large enough that
 * the per-slice overhead disappears, small enough that a 100MB attachment
 * never has more than this resident.
 */
const DIGEST_SLICE_BYTES = 4 * 1024 * 1024;

/**
 * Hex SHA-256 of the bytes about to be uploaded.
 *
 * The server stores blobs content-addressed: the digest is the unique key,
 * so the same file attached twice is stored once, and it is what the storage
 * key is built from rather than the filename. It has to be computed here
 * because it is needed before the bytes are sent -- it names the object the
 * upload URL is issued for, and when those bytes are already stored it is what
 * lets the server answer with no upload URL at all.
 *
 * A Blob is read a slice at a time: a File is backed by disk until something
 * asks for its bytes, and `crypto.subtle` can only be given all of them at
 * once, which for a 100MB attachment means 100MB resident just to name it.
 * An ArrayBuffer is already in memory, so it takes the faster native path.
 */
export async function sha256Hex(body: ArrayBuffer | Blob): Promise<string> {
  if (!(body instanceof Blob)) {
    const digest = await crypto.subtle.digest('SHA-256', body);
    return toHex(new Uint8Array(digest));
  }
  const hash = new Sha256();
  for (let offset = 0; offset < body.size; offset += DIGEST_SLICE_BYTES) {
    const slice = await body.slice(offset, offset + DIGEST_SLICE_BYTES).arrayBuffer();
    hash.update(new Uint8Array(slice));
  }
  return toHex(hash.digest());
}

interface UploadViaPresignArgs<P extends PresignResult> {
  /** Logical upload kind, used to enforce client-side size/type limits. */
  kind: UploadKind;
  /** Content type sent on the signed PUT — must match what the server signed. */
  contentType: string;
  /** Raw bytes to upload. */
  body: ArrayBuffer | Blob;
  /** Declared byte size for client-side validation. */
  byteSize: number;
  /**
   * Reserves the upload. Receives the lowercase-hex SHA-256 of `body`, which
   * every presign request declares, and returns the server's reservation.
   */
  presign: (sha256: string) => Promise<P>;
}

/** PUTs `body` to a signed URL, failing with the localized upload error. */
export async function putToSignedUrl(
  url: string,
  contentType: string,
  body: ArrayBuffer | Blob,
): Promise<void> {
  const res = await fetch(url, { method: 'PUT', headers: { 'Content-Type': contentType }, body });
  if (!res.ok) throw new ApiError(res.status, getT()('error.uploadFailed'));
}

/**
 * Runs the shared presign -> PUT -> (caller confirms) upload flow:
 * validates the file client-side, declares its digest to the presign call,
 * uploads the bytes with the matching `Content-Type`, and verifies the PUT
 * succeeded. Returns the presign response so callers can run their own
 * confirm step.
 *
 * @throws ApiError when validation, presigning, or the PUT fails.
 */
export async function uploadViaPresign<P extends PresignResult>(
  args: UploadViaPresignArgs<P>,
): Promise<P> {
  validateUpload(args.kind, args.contentType, args.byteSize);
  const presign = await args.presign(await sha256Hex(args.body));
  // Content-addressed storage answers with no upload URL when the bytes are
  // already there and something is already using them. Re-uploading would only
  // be a chance to replace a file other people's attachments point at, so the
  // reservation goes straight to its confirm step.
  if (!presign.uploadUrl) return presign;
  await putToSignedUrl(presign.uploadUrl, args.contentType, args.body);
  return presign;
}

/**
 * Sets a calendar's cover: presign, PUT, then confirm. Resolves to the
 * updated calendar.
 *
 * @throws ApiError when validation, presigning, the PUT or the confirm fails.
 */
export async function uploadCalendarCover(
  calendarId: string,
  bytes: ArrayBuffer,
  contentType: string,
): Promise<Calendar> {
  const { coverId } = await uploadViaPresign({
    kind: 'cover',
    contentType,
    body: bytes,
    byteSize: bytes.byteLength,
    presign: async (sha256) => {
      const { data } = await client.POST('/calendars/{calendarId}/cover/presign', {
        params: { path: { calendarId } },
        // validateUpload has narrowed the type to the three the endpoint takes.
        body: {
          contentType: contentType as 'image/jpeg' | 'image/png' | 'image/webp',
          byteSize: bytes.byteLength,
          sha256,
        },
      });
      return present(data);
    },
  });
  const { data } = await client.PUT('/calendars/{calendarId}/cover', {
    params: { path: { calendarId } },
    body: { coverId },
  });
  return present(data);
}
