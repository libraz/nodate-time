import { create } from 'zustand';
import { loadJson, saveJson } from '@/lib/storage';

/** Contextual tips, each shown once until dismissed. */
export type TipId = 'guide' | 'members' | 'public' | 'memo' | 'album' | 'polls' | 'event';

interface GuideRecord {
  wizardDone: boolean;
  dismissedTips: TipId[];
}

const EMPTY_RECORD: GuideRecord = { wizardDone: false, dismissedTips: [] };

interface GuideState extends GuideRecord {
  /** The account the record belongs to; null until one is signed in. */
  userId: string | null;
  showWizard: boolean;
  /** Loads the per-device record for the signed-in account. */
  loadFor: (userId: string | null) => void;
  openWizard: () => void;
  /** Closes the wizard and records it as seen, whether finished or skipped. */
  closeWizard: () => void;
  dismissTip: (id: TipId) => void;
  resetTips: () => void;
}

// Per device and per account: a shared computer shows each person their own tour.
const storageKey = (userId: string) => `guide_${userId}`;

function persist(state: GuideState) {
  if (!state.userId) return;
  saveJson<GuideRecord>(storageKey(state.userId), {
    wizardDone: state.wizardDone,
    dismissedTips: state.dismissedTips,
  });
}

export const useGuideStore = create<GuideState>((set, get) => ({
  ...EMPTY_RECORD,
  userId: null,
  showWizard: false,

  loadFor: (userId) => {
    if (get().userId === userId) return;
    const record = userId
      ? { ...EMPTY_RECORD, ...loadJson<Partial<GuideRecord>>(storageKey(userId), {}) }
      : EMPTY_RECORD;
    set({ userId, showWizard: false, ...record });
  },

  openWizard: () => set({ showWizard: true }),

  closeWizard: () => {
    // Taking the tour makes the invitation to take it redundant.
    const dismissedTips = get().dismissedTips.includes('guide')
      ? get().dismissedTips
      : [...get().dismissedTips, 'guide' as const];
    set({ showWizard: false, wizardDone: true, dismissedTips });
    persist(get());
  },

  dismissTip: (id) => {
    if (get().dismissedTips.includes(id)) return;
    set({ dismissedTips: [...get().dismissedTips, id] });
    persist(get());
  },

  resetTips: () => {
    set({ dismissedTips: [] });
    persist(get());
  },
}));

/** Whether a tip should still be shown to the signed-in account. */
export function useTipVisible(id: TipId): boolean {
  return useGuideStore((s) => s.userId !== null && !s.dismissedTips.includes(id));
}
