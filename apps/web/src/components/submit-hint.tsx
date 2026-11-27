import { useT } from '@/i18n';
import { submitModifierLabel } from '@/lib/keys';

/** Keycap hint for the Cmd/Ctrl+Enter shortcut, shown beside a primary submit button. */
export function SubmitHint({ className = '' }: { className?: string }) {
  const t = useT();
  return (
    <span className={`submit-hint ${className}`}>
      <kbd>{submitModifierLabel()}</kbd>
      <kbd>{'\u21b5'}</kbd>
      <span>{t('common.submitHint')}</span>
    </span>
  );
}
