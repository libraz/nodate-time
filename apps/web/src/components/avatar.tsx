interface AvatarProps {
  /** The picture to show. Absent falls back to the initial. */
  src?: string | undefined;
  /** Whose picture it is. Its first character stands in when there is no
   *  picture, and every place this appears already shows the name beside it,
   *  which is why the box itself is hidden from assistive technology. */
  name: string;
  /** Box size, as Tailwind sizing classes. */
  size?: string;
  /** Background behind the initial. Members carry a colour of their own;
   *  everywhere else falls back to the inset surface. */
  color?: string | undefined;
  /** Draw a circle instead of the default rounded square. */
  round?: boolean;
  className?: string;
}

/**
 * Avatar renders a person as their uploaded picture, or as the initial of
 * their name when they have none.
 *
 * A server response carries the picture as a URL, so the one thing this must
 * not do is print that string: a URL rendered as text looks like a bug on the
 * page and hides the fact that there is a picture to show.
 */
export function Avatar({ src, name, size = 'h-8 w-8', color, round, className = '' }: AvatarProps) {
  const shape = round ? 'rounded-full' : '';
  const fill = color ? 'font-bold text-white' : 'bg-[var(--color-surface-inset)]';
  return (
    <span
      aria-hidden
      className={`${size} ${shape} ${fill} flex shrink-0 items-center justify-center overflow-hidden text-default ${className}`}
      style={{
        backgroundColor: color,
        borderRadius: round ? undefined : 'var(--radius-sm)',
      }}
    >
      {src ? <img src={src} alt="" className="h-full w-full object-cover" /> : name.slice(0, 1)}
    </span>
  );
}
