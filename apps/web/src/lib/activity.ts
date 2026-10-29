import type { TranslationKey } from '@/i18n';

/**
 * The vocabulary the UI renders. It is deliberately smaller than the server's:
 * a comment being added and a photo being uploaded read the same to a person
 * looking at a feed.
 */
export type ActivityVerb =
  | 'created'
  | 'updated'
  | 'deleted'
  | 'roleChanged'
  | 'revoked'
  | 'completed'
  | 'reminded'
  | 'unknown';

/**
 * Reads the verb out of the server's dotted action name.
 *
 * The server records `calendar.event.created`, `calendar.member.role_changed`
 * and so on; matching the last segment rather than the whole string means a
 * new entity -- attachments, checklists -- arrives here already understood,
 * which is the property the dotted naming exists for.
 */
export function activityVerb(action: string): ActivityVerb {
  const last = action.split('.').pop() ?? '';
  switch (last) {
    case 'created':
    case 'added':
    case 'uploaded':
    case 'subscribed':
      return 'created';
    case 'updated':
    case 'edited':
    case 'rotated':
      return 'updated';
    case 'deleted':
    case 'removed':
      return 'deleted';
    case 'role_changed':
      return 'roleChanged';
    case 'revoked':
      return 'revoked';
    case 'completed':
      return 'completed';
    case 'reminder':
      return 'reminded';
    default:
      return 'unknown';
  }
}

const VERB_LABEL: Record<ActivityVerb, TranslationKey> = {
  created: 'history.created',
  updated: 'history.updated',
  deleted: 'history.deleted',
  roleChanged: 'activity.roleChanged',
  revoked: 'activity.revoked',
  completed: 'activity.completed',
  reminded: 'activity.reminded',
  unknown: 'activity.changed',
};

/** The label for an action, never undefined: an unrecognised verb still reads. */
export function activityLabelKey(action: string): TranslationKey {
  return VERB_LABEL[activityVerb(action)];
}

/** The colour an action is drawn in. Additions read as arrivals, removals as losses. */
export function activityColor(action: string): string {
  switch (activityVerb(action)) {
    case 'created':
      return 'var(--color-accent)';
    case 'deleted':
    case 'revoked':
      return 'var(--color-danger)';
    default:
      return 'var(--color-text-tertiary)';
  }
}
