import { create } from 'zustand';
import { errorMessage, isAbortError } from '@/lib/client';
import { type SchedulePollAction, schedulePollApi } from '@/lib/schedule-polls';

interface SchedulePollState {
  accountId: string | null;
  actions: SchedulePollAction[];
  isLoading: boolean;
  error: string | null;
  fetchActions: (accountId: string, signal?: AbortSignal) => Promise<void>;
  reset: () => void;
}

let requestSequence = 0;

export const useSchedulePollStore = create<SchedulePollState>((set) => ({
  accountId: null,
  actions: [],
  isLoading: false,
  error: null,

  async fetchActions(accountId, signal) {
    const sequence = ++requestSequence;
    set((state) => ({
      accountId,
      actions: state.accountId === accountId ? state.actions : [],
      error: null,
      isLoading: true,
    }));
    try {
      const actions = await schedulePollApi.actions(signal);
      if (sequence === requestSequence && useSchedulePollStore.getState().accountId === accountId) {
        set({ actions, error: null, isLoading: false });
      }
    } catch (error) {
      if (
        sequence !== requestSequence ||
        isAbortError(error) ||
        useSchedulePollStore.getState().accountId !== accountId
      )
        return;
      set({ error: errorMessage(error), isLoading: false });
    }
  },

  reset() {
    requestSequence += 1;
    set({ accountId: null, actions: [], isLoading: false, error: null });
  },
}));
