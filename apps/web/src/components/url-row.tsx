import { useEffect, useState } from 'react';
import { useT } from '@/i18n';
import { copyText } from '@/lib/clipboard';
import { toast } from '@/lib/toast';

const COPIED_MS = 2000;

interface UrlRowProps {
  value: string;
  copyLabel: string;
  /** Called once the clipboard write has gone through. */
  onCopied?: () => void;
}

/** A read-only link with a copy button that acknowledges only a write that landed. */
export function UrlRow({ value, copyLabel, onCopied }: UrlRowProps) {
  const t = useT();
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), COPIED_MS);
    return () => clearTimeout(timer);
  }, [copied]);

  const copy = async () => {
    if (!(await copyText(value))) {
      toast.error(t('common.copyFailed'));
      return;
    }
    setCopied(true);
    onCopied?.();
  };

  return (
    <div
      className="flex items-center gap-2 border border-[var(--color-border)] bg-[var(--color-surface-secondary)] px-3 py-2"
      style={{ borderRadius: 'var(--radius-sm)' }}
    >
      <input
        data-1p-ignore
        type="text"
        readOnly
        value={value}
        onFocus={(e) => e.currentTarget.select()}
        className="flex-1 truncate bg-transparent text-footnote text-[var(--color-text-secondary)] outline-none"
      />
      <button type="button" onClick={copy} className="btn-primary btn-sm shrink-0">
        {copied ? t('common.copied') : copyLabel}
      </button>
    </div>
  );
}
