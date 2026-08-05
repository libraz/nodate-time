import { getT } from '@/i18n';
import { ApiError, api } from '@/lib/api';

/** Server-side upload limits, mirrored on the client to fail fast before presigning. */
export const UPLOAD_LIMITS = {
  avatar: { maxBytes: 5 * 1024 * 1024, types: ['image/jpeg', 'image/png', 'image/webp'] },
  album: { maxBytes: 20 * 1024 * 1024, types: ['image/'] },
  attachment: { maxBytes: 100 * 1024 * 1024, types: [] as string[] },
} as const;

export type UploadKind = keyof typeof UPLOAD_LIMITS;

/** Returns true when `contentType` matches one of the allowed type prefixes/exacts. */
function typeAllowed(contentType: string, allowed: readonly string[]): boolean {
  if (allowed.length === 0) return true;
  return allowed.some((a) => (a.endsWith('/') ? contentType.startsWith(a) : contentType === a));
}

/** Validates `byteSize`/`contentType` against the limits for `kind`, throwing a localized error. */
export function validateUpload(kind: UploadKind, contentType: string, byteSize: number): void {
  const t = getT();
  const limit = UPLOAD_LIMITS[kind];
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
 * Hex SHA-256 of the bytes about to be uploaded.
 *
 * The server stores blobs content-addressed: the digest is the unique key,
 * so the same file attached twice is stored once, and it is what the storage
 * key is built from rather than the filename. The digest has to come from
 * the client because the server does not see the bytes -- the upload goes
 * straight to object storage through a presigned URL.
 */
export async function sha256Hex(body: ArrayBuffer | Blob): Promise<string> {
  const buffer = body instanceof Blob ? await body.arrayBuffer() : body;
  const digest = await crypto.subtle.digest('SHA-256', buffer);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
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
