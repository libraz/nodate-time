const PREFIX = 'tt_';

export function loadJson<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(`${PREFIX}${key}`);
    if (raw === null) return fallback;
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

export function saveJson<T>(key: string, value: T): void {
  localStorage.setItem(`${PREFIX}${key}`, JSON.stringify(value));
}

export function removeItem(key: string): void {
  localStorage.removeItem(`${PREFIX}${key}`);
}
