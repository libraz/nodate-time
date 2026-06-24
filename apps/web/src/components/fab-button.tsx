import { useT } from '@/i18n';
import { useUiStore } from '@/stores/ui-store';

export function FabButton() {
  const t = useT();
  const openEventModal = useUiStore((s) => s.openEventModal);

  return (
    <button
      type="button"
      onClick={() => openEventModal()}
      className="fab-button fixed z-30 flex h-[56px] w-[56px] items-center justify-center transition-transform hover:scale-105 active:scale-90 sm:hidden"
      style={{
        bottom: 'calc(60px + env(safe-area-inset-bottom))',
        right: '16px',
        borderRadius: 'var(--radius-lg)',
        background: 'var(--color-accent)',
        color: 'var(--color-text-on-accent, #fff)',
      }}
      aria-label={t('event.addEvent')}
    >
      <svg
        width="24"
        height="24"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <line x1="12" y1="5" x2="12" y2="19" />
        <line x1="5" y1="12" x2="19" y2="12" />
      </svg>
    </button>
  );
}
