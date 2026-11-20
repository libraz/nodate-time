import { Link } from '@tanstack/react-router';
import { useT } from '@/i18n';

interface FallbackProps {
  title: string;
  description: string;
  onRetry?: () => void;
}

function Fallback({ title, description, onRetry }: FallbackProps) {
  const t = useT();
  return (
    <div className="app-bg flex min-h-screen items-center justify-center p-6">
      <div className="max-w-sm text-center">
        <h1 className="text-heading font-semibold text-[var(--color-text-primary)]">{title}</h1>
        <p className="mt-2 text-body text-[var(--color-text-secondary)]">{description}</p>
        <div className="mt-5 flex items-center justify-center gap-2">
          {onRetry && (
            <button type="button" onClick={onRetry} className="btn-secondary h-9 px-4 text-body">
              {t('error.retry')}
            </button>
          )}
          <Link to="/" className="btn-primary inline-flex h-9 items-center px-4 text-body">
            {t('route.backToCalendar')}
          </Link>
        </div>
      </div>
    </div>
  );
}

/** Shown for a URL that matches no route. */
export function NotFoundPage() {
  const t = useT();
  return <Fallback title={t('route.notFoundTitle')} description={t('route.notFoundDesc')} />;
}

/** Shown when a route throws while rendering or loading. */
export function RouteErrorPage({ reset }: { reset: () => void }) {
  const t = useT();
  return (
    <Fallback title={t('route.errorTitle')} description={t('route.errorDesc')} onRetry={reset} />
  );
}
