import {
  type CSSProperties,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import { GUIDE_ICONS } from '@/components/guide';
import { type TranslationKey, useT } from '@/i18n';
import { APP_NAME } from '@/lib/brand';
import { useModalA11y } from '@/lib/use-modal-a11y';
import { useGuideStore } from '@/stores/guide-store';

interface SpotStep {
  /** Matches `data-tour`; the first visible element is the one lit. */
  target: string;
  title: TranslationKey;
  desc: TranslationKey;
}

/**
 * Pointed at in order after the welcome card. A step whose target is not on
 * screen (the phone menu on a desktop, the sidebar on a phone) is left out, so
 * each layout gets the tour of its own controls.
 */
const SPOT_STEPS: SpotStep[] = [
  { target: 'menu', title: 'tour.menuTitle', desc: 'tour.menuDesc' },
  { target: 'calendars', title: 'tour.calendarsTitle', desc: 'tour.calendarsDesc' },
  { target: 'add-event', title: 'tour.addTitle', desc: 'tour.addDesc' },
  { target: 'share', title: 'tour.shareTitle', desc: 'tour.shareDesc' },
  { target: 'help', title: 'tour.helpTitle', desc: 'tour.helpDesc' },
];

const SPOT_PAD = 6;
const CARD_GAP = 14;
const CARD_WIDTH = 320;
const VIEW_MARGIN = 12;

function findTarget(name: string): HTMLElement | null {
  for (const el of document.querySelectorAll<HTMLElement>(`[data-tour="${name}"]`)) {
    const r = el.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) return el;
  }
  return null;
}

interface Rect {
  top: number;
  left: number;
  width: number;
  height: number;
}

/** Below the target, else above, else beside it: whichever fits first. */
function placeCard(spot: Rect, cardHeight: number): CSSProperties {
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const width = Math.min(CARD_WIDTH, vw - VIEW_MARGIN * 2);
  const clampX = (x: number) => Math.min(Math.max(x, VIEW_MARGIN), vw - width - VIEW_MARGIN);
  const clampY = (y: number) => Math.min(Math.max(y, VIEW_MARGIN), vh - cardHeight - VIEW_MARGIN);
  const centeredX = clampX(spot.left + spot.width / 2 - width / 2);

  const below = spot.top + spot.height + CARD_GAP;
  if (below + cardHeight <= vh - VIEW_MARGIN) return { top: below, left: centeredX, width };
  const above = spot.top - CARD_GAP - cardHeight;
  if (above >= VIEW_MARGIN) return { top: above, left: centeredX, width };
  const right = spot.left + spot.width + CARD_GAP;
  if (right + width <= vw - VIEW_MARGIN) return { top: clampY(spot.top), left: right, width };
  return { top: clampY(spot.top), left: clampX(spot.left - CARD_GAP - width), width };
}

function Point({ title, desc }: { title: string; desc: string }) {
  return (
    <li className="flex gap-3">
      <span
        aria-hidden="true"
        className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-[var(--color-accent)]"
      />
      <span>
        <span className="block text-default font-semibold text-[var(--color-text-primary)]">
          {title}
        </span>
        <span className="block text-body text-[var(--color-text-secondary)]">{desc}</span>
      </span>
    </li>
  );
}

function Progress({ index, total }: { index: number; total: number }) {
  const t = useT();
  return (
    <div className="flex flex-1 items-center gap-2">
      <span className="flex gap-1.5" aria-hidden="true">
        {Array.from({ length: total }, (_, i) => (
          <span
            // biome-ignore lint/suspicious/noArrayIndexKey: positional dots
            key={i}
            className={`h-1.5 rounded-full transition-all ${
              i === index ? 'w-5 bg-[var(--color-accent)]' : 'w-1.5 bg-[var(--color-border-strong)]'
            }`}
          />
        ))}
      </span>
      <span className="text-footnote text-[var(--color-text-tertiary)] tabular-nums">
        {t('onboarding.step', { current: index + 1, total })}
      </span>
    </div>
  );
}

/**
 * The first-run tour: a welcome card, then a spotlight on each control a
 * newcomer needs, with the explanation beside it. Skipping counts as seen.
 */
export function OnboardingWizard() {
  const t = useT();
  const open = useGuideStore((s) => s.showWizard);
  const closeWizard = useGuideStore((s) => s.closeWizard);
  const [index, setIndex] = useState(0);
  const [steps, setSteps] = useState<SpotStep[]>([]);
  const [spot, setSpot] = useState<Rect | null>(null);
  const [cardHeight, setCardHeight] = useState(200);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const nextRef = useRef<HTMLButtonElement>(null);
  const titleId = useId();
  const descId = useId();

  const close = useCallback(() => {
    closeWizard();
    setIndex(0);
    setSpot(null);
  }, [closeWizard]);
  const panelRef = useModalA11y<HTMLDivElement>(open, close, nextRef);

  // Which controls exist is a question for the layout on screen when it opens.
  useEffect(() => {
    if (open) setSteps(SPOT_STEPS.filter((s) => findTarget(s.target)));
  }, [open]);

  // Each step mounts a new card, so focus follows the primary button onto it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-runs per step on purpose
  useEffect(() => {
    if (open) nextRef.current?.focus();
  }, [open, index]);

  const total = steps.length + 1;
  const current = index > 0 ? steps[index - 1] : undefined;

  const measure = useCallback(() => {
    const el = current && findTarget(current.target);
    if (!el) {
      setSpot(null);
      return;
    }
    const r = el.getBoundingClientRect();
    // Kept on screen, so a control against the edge still gets a whole ring.
    const top = Math.max(r.top - SPOT_PAD, 2);
    const left = Math.max(r.left - SPOT_PAD, 2);
    const bottom = Math.min(r.bottom + SPOT_PAD, window.innerHeight - 2);
    const right = Math.min(r.right + SPOT_PAD, window.innerWidth - 2);
    setSpot({ top, left, width: right - left, height: bottom - top });
  }, [current]);

  useLayoutEffect(() => {
    if (!open) return;
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [open, measure]);

  // The card is placed by its own height, which only the rendered card knows.
  useLayoutEffect(() => {
    const h = cardRef.current?.offsetHeight;
    if (h && h !== cardHeight) setCardHeight(h);
  });

  if (!open) return null;

  const last = index === total - 1;
  const footer = (
    <div className="flex items-center gap-2">
      <Progress index={index} total={total} />
      {index > 0 && (
        <button
          type="button"
          onClick={() => setIndex(index - 1)}
          className="btn-secondary h-9 px-3.5 text-body font-medium"
        >
          {t('onboarding.back')}
        </button>
      )}
      <button
        ref={nextRef}
        type="button"
        onClick={() => (last ? close() : setIndex(index + 1))}
        className="btn-primary h-9 min-w-[84px] px-4 text-body font-medium"
      >
        {last ? t('onboarding.finish') : t('onboarding.next')}
      </button>
    </div>
  );

  const skip = (
    <button
      type="button"
      onClick={close}
      className="shrink-0 rounded-lg px-2 py-1 text-body text-[var(--color-text-secondary)] hover:bg-[var(--color-hover)]"
    >
      {t('onboarding.skip')}
    </button>
  );

  const setPanel = (el: HTMLDivElement | null) => {
    panelRef.current = el;
    cardRef.current = el;
  };

  if (!current) {
    return (
      <>
        <div className="modal-backdrop fixed inset-0 z-[70] bg-[var(--color-overlay)]" />
        <div className="fixed inset-0 z-[70] flex items-center justify-center p-3 sm:p-4">
          <div
            ref={setPanel}
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            className="glass-surface-heavy modal-panel flex w-full max-w-[480px] flex-col ring-1 ring-[var(--color-border)]"
          >
            <div className="flex items-center justify-between px-6 pt-5">
              <span className="text-footnote font-medium text-[var(--color-text-tertiary)]">
                {t('onboarding.label')}
              </span>
              {skip}
            </div>
            <div className="px-6 pb-6 pt-4">
              <span
                aria-hidden="true"
                className="mb-5 flex h-14 w-14 items-center justify-center rounded-2xl text-[var(--color-accent-ink)]"
                style={{ background: 'color-mix(in srgb, var(--color-accent) 12%, transparent)' }}
              >
                <span className="h-7 w-7">{GUIDE_ICONS.calendar}</span>
              </span>
              <h2 id={titleId} className="text-heading font-bold text-[var(--color-text-primary)]">
                {t('onboarding.welcomeTitle', { app: APP_NAME })}
              </h2>
              <p className="mt-2 text-default leading-relaxed text-[var(--color-text-secondary)]">
                {t('onboarding.welcomeLead')}
              </p>
              <ul className="mt-6 space-y-4">
                <Point title={t('onboarding.pointWrite')} desc={t('onboarding.pointWriteDesc')} />
                <Point title={t('onboarding.pointShare')} desc={t('onboarding.pointShareDesc')} />
                <Point title={t('onboarding.pointDecide')} desc={t('onboarding.pointDecideDesc')} />
              </ul>
            </div>
            <div className="border-t border-[var(--color-separator)] px-6 py-4">{footer}</div>
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      {/* Takes every click: the page behind is shown, not operated. */}
      <div className="fixed inset-0 z-[70]" />
      {spot && (
        <div
          aria-hidden="true"
          className="pointer-events-none fixed z-[70] rounded-[14px] motion-safe:transition-all motion-safe:duration-300"
          style={{
            ...spot,
            boxShadow:
              '0 0 0 2px var(--color-accent), 0 0 0 9999px color-mix(in srgb, #000 55%, transparent)',
          }}
        />
      )}
      <div
        ref={setPanel}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descId}
        className="glass-surface-heavy fixed z-[71] rounded-[var(--radius-lg)] p-4 ring-1 ring-[var(--color-border)]"
        style={
          spot
            ? placeCard(spot, cardHeight)
            : { top: '50%', left: '50%', width: CARD_WIDTH, transform: 'translate(-50%, -50%)' }
        }
      >
        <div className="mb-1 flex items-start justify-between gap-2">
          <h2 id={titleId} className="pt-1 text-callout font-bold text-[var(--color-text-primary)]">
            {t(current.title)}
          </h2>
          {skip}
        </div>
        <p
          id={descId}
          className="mb-4 text-body leading-relaxed text-[var(--color-text-secondary)]"
        >
          {t(current.desc)}
        </p>
        {footer}
      </div>
    </>
  );
}
