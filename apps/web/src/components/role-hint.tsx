import { useT } from '@/i18n';

const ROLE_HINT_KEY = {
  editor: 'members.roleEditorHint',
  viewer: 'members.roleViewerHint',
} as const;

/** What the selected role allows, shown under the control that picks it. */
export function RoleHint({ role, className = '' }: { role: string; className?: string }) {
  const t = useT();
  const key = ROLE_HINT_KEY[role as keyof typeof ROLE_HINT_KEY];
  if (!key) return null;
  return (
    <p className={`text-footnote text-[var(--color-text-secondary)] ${className}`}>{t(key)}</p>
  );
}
