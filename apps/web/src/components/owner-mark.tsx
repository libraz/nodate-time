import { readableInk } from '@/lib/member-color';

/**
 * Whose layer an event sits on, as a small disc in the owner's colour: their
 * picture when they have one, their initial otherwise. Colour alone cannot
 * tell members apart once two of them pick the same one, so the person is
 * shown as well. An event whose owner has left carries no name and falls back
 * to a plain dot.
 */
export function OwnerMark({
  name,
  avatarUrl,
  color,
  size,
  inverse = false,
  className = '',
}: {
  name?: string | undefined;
  avatarUrl?: string | undefined;
  color: string;
  /** Diameter in pixels. */
  size: number;
  /** Swap fill and ink, for a mark sitting on a bar already filled with the colour. */
  inverse?: boolean;
  className?: string;
}) {
  const initial = name?.trim().slice(0, 1);
  const ink = readableInk(color);
  if (!initial) {
    const dot = Math.max(4, Math.round(size / 2.5));
    return (
      <span
        aria-hidden
        className={`inline-flex shrink-0 items-center justify-center ${className}`}
        style={{ width: size, height: size }}
      >
        <span
          className="rounded-full"
          style={{ width: dot, height: dot, backgroundColor: inverse ? ink : color }}
        />
      </span>
    );
  }
  return (
    <span
      aria-hidden
      title={name}
      className={`owner-mark inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full font-bold leading-none ${className}`}
      style={{
        width: size,
        height: size,
        fontSize: Math.round(size * 0.58),
        backgroundColor: inverse ? ink : color,
        color: inverse ? color : ink,
      }}
    >
      {avatarUrl ? <img src={avatarUrl} alt="" className="h-full w-full object-cover" /> : initial}
    </span>
  );
}
