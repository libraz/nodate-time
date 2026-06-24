import { useToastStore } from '@/lib/toast';

export function Toaster() {
  const toasts = useToastStore((s) => s.toasts);
  const dismiss = useToastStore((s) => s.dismiss);

  if (toasts.length === 0) return null;

  return (
    <section
      className="pointer-events-none fixed inset-x-0 bottom-6 z-[60] flex flex-col items-center gap-2 px-4"
      aria-live="polite"
      aria-label="Notifications"
    >
      {toasts.map((t) => (
        <button
          key={t.id}
          type="button"
          onClick={() => dismiss(t.id)}
          className={`toast-item pointer-events-auto flex max-w-[420px] items-center gap-3 rounded-2xl px-4 py-3 text-default shadow-lg ring-1 backdrop-blur transition ${
            t.tone === 'success'
              ? 'bg-[var(--color-accent-bg)]/95 text-[var(--color-accent)] ring-[var(--color-accent)]/30'
              : t.tone === 'error'
                ? 'bg-[var(--color-danger-bg)]/95 text-[var(--color-danger)] ring-[var(--color-danger)]/30'
                : 'bg-[var(--color-surface)]/95 text-[var(--color-text-primary)] ring-[var(--color-border)]'
          }`}
        >
          <span aria-hidden className="shrink-0">
            {t.tone === 'success' ? (
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none">
                <path
                  d="M5 13l4 4L19 7"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            ) : t.tone === 'error' ? (
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
          <span className="text-left">{t.message}</span>
        </button>
      ))}
    </section>
  );
}
