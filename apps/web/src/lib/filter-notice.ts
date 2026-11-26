import { getT } from '@/i18n';
import { toast } from '@/lib/toast';

/**
 * Tells the user a save went through although the filter hides its result,
 * so an event that vanishes on save does not read as a failed save.
 */
export function offerFilterReveal(clear: () => void): void {
  const t = getT();
  toast.info(t('filter.savedHidden'), { label: t('filter.clear'), run: clear });
}
