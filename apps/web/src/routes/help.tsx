import { createFileRoute, Link, useLocation, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';
import { BrandRow } from '@/components/brand-mark';
import { useT } from '@/i18n';
import { HELP_SECTIONS, type HelpBlock } from '@/lib/help-topics';
import { toast } from '@/lib/toast';
import { useGuideStore } from '@/stores/guide-store';

export const Route = createFileRoute('/help')({
  component: HelpPage,
});

function HelpBlockView({ block }: { block: HelpBlock }) {
  const t = useT();
  switch (block.kind) {
    case 'lead':
      return (
        <p className="text-callout leading-relaxed text-[var(--color-text-primary)]">
          {t(block.text)}
        </p>
      );
    case 'steps':
      return (
        <ol className="space-y-3">
          {block.items.map((key, i) => (
            <li key={key} className="flex gap-3">
              <span
                aria-hidden="true"
                className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--color-accent)] text-footnote font-semibold text-[var(--color-text-on-accent)] tabular-nums"
              >
                {i + 1}
              </span>
              <span className="pt-0.5 text-default leading-relaxed text-[var(--color-text-primary)]">
                {t(key)}
              </span>
            </li>
          ))}
        </ol>
      );
    case 'terms':
      return (
        <div>
          {block.heading && (
            <h3 className="mb-2 text-default font-semibold text-[var(--color-text-primary)]">
              {t(block.heading)}
            </h3>
          )}
          <dl className="divide-y divide-[var(--color-separator)]">
            {block.items.map(({ term, desc }) => (
              <div key={term} className="py-3 first:pt-0 last:pb-0">
                <dt className="text-default font-semibold text-[var(--color-text-primary)]">
                  {t(term)}
                </dt>
                <dd className="mt-1 text-body leading-relaxed text-[var(--color-text-secondary)]">
                  {t(desc)}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      );
    case 'faq':
      return (
        <div className="divide-y divide-[var(--color-separator)]">
          {block.items.map(({ q, a }) => (
            <details key={q} className="group py-3 first:pt-0 last:pb-0">
              <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-default font-semibold text-[var(--color-text-primary)]">
                {t(q)}
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  aria-hidden="true"
                  className="shrink-0 text-[var(--color-text-tertiary)] transition-transform group-open:rotate-180"
                >
                  <path d="M6 9l6 6 6-6" />
                </svg>
              </summary>
              <p className="mt-2 text-body leading-relaxed text-[var(--color-text-secondary)]">
                {t(a)}
              </p>
            </details>
          ))}
        </div>
      );
  }
}

function HelpPage() {
  const t = useT();
  const navigate = useNavigate();
  const hash = useLocation({ select: (l) => l.hash });
  const openWizard = useGuideStore((s) => s.openWizard);
  const resetTips = useGuideStore((s) => s.resetTips);

  // The page scrolls inside its own column, so the fragment is followed by hand.
  useEffect(() => {
    if (hash) document.getElementById(hash)?.scrollIntoView({ block: 'start' });
  }, [hash]);

  const replayGuide = () => {
    openWizard();
    void navigate({ to: '/' });
  };

  const handleResetTips = () => {
    resetTips();
    toast.info(t('help.tipsReset'));
  };

  return (
    <div className="app-bg h-full">
      <div className="mx-auto flex h-full max-w-[1080px] flex-col px-4 py-6 sm:px-6">
        <header className="mb-6 flex items-center justify-between gap-4">
          <div className="min-w-0">
            <div className="mb-3">
              <BrandRow />
            </div>
            <h1 className="text-display font-bold text-[var(--color-text-primary)] sm:text-hero">
              {t('help.title')}
            </h1>
            <p className="mt-1 text-default text-[var(--color-text-secondary)]">{t('help.lead')}</p>
          </div>
          <Link
            to="/"
            className="shrink-0 whitespace-nowrap rounded-full bg-[var(--color-surface-inset)] px-4 py-2 text-body font-medium text-[var(--color-text-primary)] transition hover:bg-[var(--color-hover)]"
          >
            {t('common.close')}
          </Link>
        </header>

        <div className="flex flex-1 gap-6 overflow-hidden">
          <nav aria-label={t('help.contents')} className="hidden w-[240px] shrink-0 sm:block">
            <p className="mb-2 px-3 text-footnote font-medium text-[var(--color-text-tertiary)]">
              {t('help.contents')}
            </p>
            <ul className="flex flex-col gap-0.5">
              {HELP_SECTIONS.map((s) => (
                <li key={s.id}>
                  <Link
                    to="/help"
                    hash={s.id}
                    aria-current={hash === s.id ? 'location' : undefined}
                    className={`block rounded-xl px-3 py-2 text-default transition ${
                      hash === s.id
                        ? 'bg-[var(--color-accent-bg)] font-semibold text-[var(--color-accent-ink)]'
                        : 'text-[var(--color-text-primary)] hover:bg-[var(--color-hover)]'
                    }`}
                  >
                    {t(s.title)}
                  </Link>
                </li>
              ))}
            </ul>
          </nav>

          <main className="calendar-enter flex-1 overflow-y-auto pb-12 sm:pr-2">
            {/* Mobile table of contents */}
            <nav aria-label={t('help.contents')} className="mb-5 flex flex-wrap gap-2 sm:hidden">
              {HELP_SECTIONS.map((s) => (
                <Link
                  key={s.id}
                  to="/help"
                  hash={s.id}
                  className="rounded-full bg-[var(--color-surface-inset)] px-3.5 py-1.5 text-body font-medium text-[var(--color-text-primary)]"
                >
                  {t(s.title)}
                </Link>
              ))}
            </nav>

            {HELP_SECTIONS.map((s) => (
              <section key={s.id} id={s.id} className="mb-6 scroll-mt-2">
                <h2 className="mb-3 text-subhead font-semibold text-[var(--color-text-primary)]">
                  {t(s.title)}
                </h2>
                <div className="space-y-5 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5 shadow-sm">
                  {s.blocks.map((block, i) => (
                    // Blocks are a fixed list; their position is their identity.
                    // biome-ignore lint/suspicious/noArrayIndexKey: static content
                    <HelpBlockView key={i} block={block} />
                  ))}
                </div>
              </section>
            ))}

            <div className="flex flex-col gap-2 sm:flex-row">
              <button
                type="button"
                onClick={replayGuide}
                className="btn-primary h-10 px-5 text-default font-medium"
              >
                {t('help.replayGuide')}
              </button>
              <button
                type="button"
                onClick={handleResetTips}
                className="btn-secondary h-10 px-5 text-default font-medium"
              >
                {t('help.resetTips')}
              </button>
            </div>
          </main>
        </div>
      </div>
    </div>
  );
}
