import { create } from 'zustand';

export type ToastTone = 'info' | 'success' | 'error';

/** A single follow-up a toast offers, such as undoing what it reports. */
export interface ToastAction {
  label: string;
  run: () => void;
}

export interface Toast {
  id: number;
  tone: ToastTone;
  message: string;
  action?: ToastAction;
  /** Set while the exit animation plays, before the toast is removed. */
  leaving?: boolean;
}

interface ToastState {
  toasts: Toast[];
  show: (message: string, tone?: ToastTone, action?: ToastAction) => void;
  dismiss: (id: number) => void;
}

/** How long a toast stays up; one with an action stays longer, so it can be reached. */
const TOAST_MS = 4000;
const ACTION_TOAST_MS = 6500;
/** Matches the toast-out animation in index.css. */
const EXIT_MS = 180;

let nextId = 1;

export const useToastStore = create<ToastState>((set, get) => ({
  toasts: [],
  show: (message, tone = 'info', action) => {
    const id = nextId++;
    const item: Toast = action ? { id, tone, message, action } : { id, tone, message };
    set((s) => ({ toasts: [...s.toasts, item] }));
    setTimeout(() => get().dismiss(id), action ? ACTION_TOAST_MS : TOAST_MS);
  },
  dismiss: (id) => {
    const target = get().toasts.find((t) => t.id === id);
    if (!target || target.leaving) return;
    set((s) => ({ toasts: s.toasts.map((t) => (t.id === id ? { ...t, leaving: true } : t)) }));
    setTimeout(() => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })), EXIT_MS);
  },
}));

export const toast = {
  info: (message: string, action?: ToastAction) =>
    useToastStore.getState().show(message, 'info', action),
  success: (message: string) => useToastStore.getState().show(message, 'success'),
  error: (message: string) => useToastStore.getState().show(message, 'error'),
};
