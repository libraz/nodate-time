import { useT } from '@/i18n';
import { useUiStore } from '@/stores/ui-store';

/**
 * The phone's create-event button.
 *
 * It only stands for something on the calendar tab: on memo, search and
 * settings there is nothing on screen a new event would be added to, and the
 * memo tab has an add button of its own that this one sits on top of.
 */
export function FabButton() {
  const t = useT();
  const openEventModal = useUiStore((s) => s.openEventModal);
  const mobileTab = useUiStore((s) => s.mobileTab);

  if (mobileTab !== 'calendar') return null;

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
