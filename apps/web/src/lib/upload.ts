import { getT } from '@/i18n';
import { ApiError, api } from '@/lib/api';
import { Sha256, toHex } from '@/lib/sha256';

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
  uploadUrl: string;
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
 * key is built from rather than the filename. The digest has to come from
 * the client because the server does not see the bytes -- the upload goes
 * straight to object storage through a presigned URL.
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

interface UploadViaPresignArgs {
  /** Logical upload kind, used to enforce client-side size/type limits. */
  kind: UploadKind;
  /** Presign request path. */
  presignPath: string;
  /** Presign request body (must declare matching contentType/byteSize to the server). */
  presignBody: Record<string, unknown>;
  /** Content type sent on the signed PUT — must match what the server signed. */
  contentType: string;
  /** Raw bytes to upload. */
  body: ArrayBuffer | Blob;
  /** Declared byte size for client-side validation. */
  byteSize: number;
  /**
   * When true, the digest of `body` is computed and sent as `sha256` in the
   * presign request. Set for endpoints backed by content-addressed storage
   * (avatars, event attachments); album photos keep their own key and do not
   * take one.
   */
  contentAddressed?: boolean;
}

/**
 * Runs the shared presign -> PUT -> (caller confirms) upload flow:
 * validates the file client-side, requests a presigned URL, uploads the bytes
 * with the matching `Content-Type`, and verifies the PUT succeeded.
 * Returns the presign response so callers can run their own confirm step.
 *
 * @throws ApiError when validation, presigning, or the PUT fails.
 */
export async function uploadViaPresign<P extends PresignResult>(
  args: UploadViaPresignArgs,
): Promise<P> {
  validateUpload(args.kind, args.contentType, args.byteSize);
  const presignBody = args.contentAddressed
    ? { ...args.presignBody, sha256: await sha256Hex(args.body) }
    : args.presignBody;
  const presign = await api.post<P>(args.presignPath, presignBody);
  // Content-addressed storage answers with no upload URL when the bytes are
  // already there and something is already using them. Re-uploading would only
  // be a chance to replace a file other people's attachments point at, so the
  // reservation goes straight to its confirm step.
  if (!presign.uploadUrl) return presign;
  const putRes = await fetch(presign.uploadUrl, {
    method: 'PUT',
    headers: { 'Content-Type': args.contentType },
    body: args.body,
  });
  if (!putRes.ok) {
    throw new ApiError(putRes.status, getT()('error.uploadFailed'));
  }
  return presign;
}
