/** `text` with the first case-insensitive match of `query` marked. */
export function Highlight({ text, query }: { text: string; query: string }) {
  const q = query.trim().toLowerCase();
  const lower = text.toLowerCase();
  const at = q ? lower.indexOf(q) : -1;
  // Lower-casing can change a string's length; marking by offsets is then wrong.
  if (at < 0 || lower.length !== text.length) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <mark className="search-mark">{text.slice(at, at + q.length)}</mark>
      {text.slice(at + q.length)}
    </>
  );
}

/**
 * Arrow keys walk a result list by moving focus: down from the box enters the
 * list, up from its first row returns to the box. Real focus rather than a
 * painted cursor, so Enter on a row is its own click and a screen reader
 * follows. Enter in the box opens the top result.
 */
export function handleResultListKeys(e: React.KeyboardEvent<HTMLElement>) {
  // Where a surface draws more than one layout, only the one the key came from.
  const scope =
    (e.target as HTMLElement).closest<HTMLElement>('[data-search-layout]') ?? e.currentTarget;
  const rows = Array.from(scope.querySelectorAll<HTMLElement>('[data-search-result]'));
  if (rows.length === 0) return;
  const box = scope.querySelector<HTMLInputElement>('input');
  if (e.key === 'Enter' && e.target === box) {
    e.preventDefault();
    rows[0]?.click();
    return;
  }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
  const at = rows.indexOf(document.activeElement as HTMLElement);
  e.preventDefault();
  if (e.key === 'ArrowDown') rows[Math.min(at + 1, rows.length - 1)]?.focus();
  else if (at <= 0) box?.focus();
  else rows[at - 1]?.focus();
}
