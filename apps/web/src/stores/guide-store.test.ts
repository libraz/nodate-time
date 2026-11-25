import { beforeEach, describe, expect, it } from 'vitest';
import { useGuideStore } from './guide-store';

const reset = () => {
  localStorage.clear();
  useGuideStore.setState({ userId: null, wizardDone: false, dismissedTips: [], showWizard: false });
};

describe('guide store', () => {
  beforeEach(reset);

  it('keeps each account on the device apart', () => {
    const s = useGuideStore.getState();
    s.loadFor('alice');
    s.dismissTip('members');
    s.openWizard();
    useGuideStore.getState().closeWizard();

    useGuideStore.getState().loadFor('bob');
    expect(useGuideStore.getState()).toMatchObject({ wizardDone: false, dismissedTips: [] });

    useGuideStore.getState().loadFor('alice');
    expect(useGuideStore.getState().wizardDone).toBe(true);
    expect(useGuideStore.getState().dismissedTips).toContain('members');
  });

  it('records a skipped tour as seen and retires its invitation', () => {
    useGuideStore.getState().loadFor('alice');
    useGuideStore.getState().openWizard();
    useGuideStore.getState().closeWizard();

    const state = useGuideStore.getState();
    expect(state.showWizard).toBe(false);
    expect(state.wizardDone).toBe(true);
    expect(state.dismissedTips).toContain('guide');
  });

  it('brings dismissed tips back on request without replaying the tour', () => {
    useGuideStore.getState().loadFor('alice');
    useGuideStore.getState().closeWizard();
    useGuideStore.getState().dismissTip('public');
    useGuideStore.getState().resetTips();

    // Reloading proves the reset reached storage, not only memory.
    useGuideStore.setState({ userId: null });
    useGuideStore.getState().loadFor('alice');
    expect(useGuideStore.getState()).toMatchObject({ wizardDone: true, dismissedTips: [] });
  });

  it('remembers nothing while signed out', () => {
    useGuideStore.getState().dismissTip('memo');
    expect(localStorage.length).toBe(0);
  });
});
