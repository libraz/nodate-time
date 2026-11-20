/** A few pulsing placeholder rows, for a list whose content is still on its way. */
export function LoadingRows({ rows = 3 }: { rows?: number }) {
  return (
    <ul className="divide-y divide-[var(--color-separator)]" aria-busy="true">
      {Array.from({ length: rows }, (_, i) => i).map((i) => (
        <li key={i} className="flex animate-pulse items-center gap-3 px-5 py-3">
          <span className="h-9 w-9 shrink-0 rounded-full bg-[var(--color-surface-inset)]" />
          <span className="h-4 flex-1 rounded bg-[var(--color-surface-inset)]" />
        </li>
      ))}
    </ul>
  );
}
