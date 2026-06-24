import '@testing-library/jest-dom/vitest';

// jsdom does not implement scrollIntoView
Element.prototype.scrollIntoView = () => {};

// Provide a clean, fully spec-compliant in-memory localStorage so tests get
// isolated, predictable storage with a working clear().
class InMemoryStorage implements Storage {
  private store = new Map<string, string>();
  get length(): number {
    return this.store.size;
  }
  clear(): void {
    this.store.clear();
  }
  getItem(key: string): string | null {
    return this.store.has(key) ? (this.store.get(key) as string) : null;
  }
  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }
  removeItem(key: string): void {
    this.store.delete(key);
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

// Wrap in a Proxy so `Object.keys(localStorage)` enumerates stored keys, the way
// the real Web Storage API exposes them (code under test relies on this).
const storageImpl = new InMemoryStorage();
const RESERVED = new Set(['length', 'clear', 'getItem', 'key', 'removeItem', 'setItem', 'store']);

const localStorageProxy = new Proxy(storageImpl, {
  ownKeys(target) {
    const keys: string[] = [];
    for (let i = 0; i < target.length; i++) {
      const k = target.key(i);
      if (k !== null) keys.push(k);
    }
    return keys;
  },
  getOwnPropertyDescriptor(target, prop) {
    if (typeof prop === 'string' && !RESERVED.has(prop) && target.getItem(prop) !== null) {
      return { enumerable: true, configurable: true, value: target.getItem(prop) };
    }
    return undefined;
  },
});

Object.defineProperty(globalThis, 'localStorage', {
  value: localStorageProxy,
  configurable: true,
  writable: true,
});
