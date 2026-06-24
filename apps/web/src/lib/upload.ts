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
  const presign = await api.post<P>(args.presignPath, args.presignBody);
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
