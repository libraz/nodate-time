import { useT } from '@/i18n';
import { useUiStore } from '@/stores/ui-store';

export function FabButton() {
  const t = useT();
  const openEventModal = useUiStore((s) => s.openEventModal);

  return (
    <button
      type="button"
      onClick={() => openEventModal()}
      className="fixed bottom-6 right-4 z-30 flex h-[60px] w-[60px] items-center justify-center rounded-[var(--radius-lg)] transition-transform hover:scale-105 active:scale-95"
      style={{
        background: 'linear-gradient(135deg, var(--color-accent), var(--color-accent-hover))',
        boxShadow: 'var(--shadow-elevated), 0 8px 24px rgba(99, 102, 241, 0.3)',
        marginBottom: 'env(safe-area-inset-bottom)',
      }}
      aria-label={t('event.addEvent')}
    >
      <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="2.5">
        <path d="M12 5v14M5 12h14" />
      </svg>
    </button>
  );
}
