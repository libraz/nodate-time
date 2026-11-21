import { ApiError } from '@/lib/api';

/** Whether a write was refused because the item changed after it was read: every such refusal is a 412. */
export function isStaleError(err: unknown): boolean {
  return err instanceof ApiError && err.status === 412;
}
