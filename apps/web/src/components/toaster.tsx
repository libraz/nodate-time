import { useT } from '@/i18n';
import { useToastStore } from '@/lib/toast';

export function Toaster() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);
  const t = useT();

  if (toasts.length === 0) return null;

  return (
    <section
      className="pointer-events-none fixed inset-x-0 bottom-6 z-[60] flex flex-col items-center gap-2 px-4"
      aria-live="polite"
      aria-label={t('common.notifications')}
    >
      {toasts.map((item) => (
        <div
          key={item.id}
          data-leaving={item.leaving || undefined}
          className={`toast-item pointer-events-auto flex max-w-[440px] items-center gap-1 rounded-2xl py-1.5 pl-1.5 pr-1.5 text-default shadow-lg ring-1 backdrop-blur ${
            item.tone === 'success'
              ? 'bg-[var(--color-accent-bg)]/95 text-[var(--color-accent)] ring-[var(--color-accent)]/30'
              : item.tone === 'error'
                ? 'bg-[var(--color-danger-bg)]/95 text-[var(--color-danger)] ring-[var(--color-danger)]/30'
                : 'bg-[var(--color-surface)]/95 text-[var(--color-text-primary)] ring-[var(--color-border)]'
          }`}
        >
          <button
            type="button"
            onClick={() => dismiss(item.id)}
            className="flex min-w-0 items-center gap-3 rounded-xl px-2.5 py-1.5 text-left"
          >
            <span aria-hidden className="shrink-0">
              {item.tone === 'success' ? (
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
                  <path
                    d="M5 13l4 4L19 7"
                    stroke="currentColor"
                    strokeWidth="2.4"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  />
                </svg>
              ) : item.tone === 'error' ? (
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
                  <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2" />
                  <path
                    d="M12 8v4M12 16h.01"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                  />
                </svg>
              ) : (
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
                  <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2" />
                  <path
                    d="M12 16v-4M12 8h.01"
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                  />
                </svg>
              )}
            </span>
            <span className="text-left">{item.message}</span>
          </button>
          {item.action && (
            <button
              type="button"
              onClick={() => {
                item.action?.run();
                dismiss(item.id);
              }}
              className="shrink-0 rounded-xl px-3 py-1.5 font-semibold text-[var(--color-accent)] transition-colors hover:bg-[var(--color-accent-bg)] active:scale-95"
            >
              {item.action.label}
            </button>
          )}
        </div>
      ))}
    </section>
  );
}
