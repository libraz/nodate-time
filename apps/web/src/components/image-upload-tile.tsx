import { useEffect, useState } from 'react';

interface ImageUploadTileProps {
  /** Accessible name of the tile; the tile opens the file picker. */
  label: string;
  /** MIME list handed to the file input. */
  accept: string;
  /** Uploads a picked or dropped file; the tile shows it locally until this settles. */
  onFile: (file: File) => Promise<void>;
  /** Busy for a reason the tile did not start (e.g. a removal). */
  busy?: boolean;
  /** Whether a current image is shown; without one the tile draws an empty drop target. */
  hasImage: boolean;
  /** The file input, so a separate text button can open the same picker. */
  inputRef: React.RefObject<HTMLInputElement | null>;
  /** Size and radius of the tile. */
  className: string;
  /** The current image, or the placeholder content when there is none. */
  children: React.ReactNode;
}

/**
 * A square image picker: click or drop a file, see it at once, and watch it
 * upload in place. The picked file is previewed from a local object URL, so
 * the tile never shows the old image while the new one is on its way.
 */
export function ImageUploadTile({
  label,
  accept,
  onFile,
  busy = false,
  hasImage,
  inputRef,
  className,
  children,
}: ImageUploadTileProps) {
  const [pendingUrl, setPendingUrl] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const working = busy || pendingUrl !== null;

  useEffect(
    () => () => {
      if (pendingUrl) URL.revokeObjectURL(pendingUrl);
    },
    [pendingUrl],
  );

  const accepts = (file: File) => {
    const types = accept.split(',').map((type) => type.trim());
    return !file.type || types.includes(file.type);
  };

  const start = async (file: File) => {
    if (working) return;
    setPendingUrl(accepts(file) ? URL.createObjectURL(file) : null);
    try {
      await onFile(file);
    } finally {
      setPendingUrl(null);
    }
  };

  return (
    <>
      <button
        type="button"
        aria-label={label}
        aria-busy={working}
        disabled={working}
        onClick={() => inputRef.current?.click()}
        onDragOver={(e) => {
          if (working || !e.dataTransfer.types.includes('Files')) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = 'copy';
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          const file = e.dataTransfer.files[0];
          if (file) void start(file);
        }}
        className={`image-upload-tile group relative shrink-0 overflow-hidden ${className}`}
        data-empty={!hasImage && !pendingUrl ? '' : undefined}
        data-dragging={dragging ? '' : undefined}
      >
        {children}
        {pendingUrl && (
          <img src={pendingUrl} alt="" className="absolute inset-0 h-full w-full object-cover" />
        )}
        <span className="image-upload-tile-overlay" aria-hidden="true">
          {working ? (
            <span className="spinner h-5 w-5" />
          ) : (
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M4 8.5A2.5 2.5 0 0 1 6.5 6h1.6l1.4-2h5l1.4 2h1.6A2.5 2.5 0 0 1 20 8.5v8a2.5 2.5 0 0 1-2.5 2.5h-11A2.5 2.5 0 0 1 4 16.5z" />
              <circle cx="12" cy="12.5" r="3.5" />
            </svg>
          )}
        </span>
      </button>
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
        onChange={(e) => {
          const file = e.currentTarget.files?.[0];
          e.currentTarget.value = '';
          if (file) void start(file);
        }}
      />
    </>
  );
}
