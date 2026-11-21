import { useT } from '@/i18n';
import { useBuildStore } from '@/lib/build-check';

/** A toast-row offering a reload once a newer build has been deployed; it stays until used. */
export function NewBuildBanner() {
  const t = useT();
  const hasNewBuild = useBuildStore((s) => s.hasNewBuild);
  if (!hasNewBuild) return null;

  return (
    <div className="pointer-events-auto flex max-w-[440px] items-center gap-1 rounded-[var(--radius-lg)] bg-[var(--color-surface)]/95 py-1.5 pl-4 pr-1.5 text-default text-[var(--color-text-primary)] shadow-lg ring-1 ring-[var(--color-border)] backdrop-blur">
      <span>{t('common.newVersionAvailable')}</span>
      <button
        type="button"
        onClick={() => location.reload()}
        className="shrink-0 rounded-xl px-3 py-1.5 font-semibold text-[var(--color-accent-ink)] transition-colors hover:bg-[var(--color-accent-bg)] active:scale-95"
      >
        {t('common.reload')}
      </button>
    </div>
  );
}
