import type { TranslationKey } from '@/i18n';
import type { Flexibility, ShowAs } from '@/types/calendar';

/** Modifier class for an availability state; busy is the unmodified look. */
export function showAsClass(showAs: ShowAs): '' | 'evt-tentative' | 'evt-free' | 'evt-oof' {
  switch (showAs) {
    case 'tentative':
      return 'evt-tentative';
    case 'free':
      return 'evt-free';
    case 'oof':
      return 'evt-oof';
    default:
      return '';
  }
}

/**
 * i18n key for a flexibility label, or `null` for fixed (nothing to say).
 * `short` selects the compact wording used in list rows.
 */
export function flexibilityLabelKey(
  flex: Flexibility,
  { short = false }: { short?: boolean } = {},
): TranslationKey | null {
  switch (flex) {
    case 'negotiable':
      return short ? 'event.flexibilityShort.negotiable' : 'event.flexibilityNegotiable';
    case 'conditional':
      return short ? 'event.flexibilityShort.conditional' : 'event.flexibilityConditional';
    default:
      return null;
  }
}
