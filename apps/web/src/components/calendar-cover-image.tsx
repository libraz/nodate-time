import { useState } from 'react';
import type { Calendar } from '@/types/calendar';

interface CalendarCoverImageProps {
  calendar: Pick<Calendar, 'color' | 'coverImageUrl'>;
  alt?: string;
  className: string;
  fallbackClassName?: string;
  fallbackColor?: string;
  inactive?: boolean;
}

/** Renders an uploaded cover image or the calendar color dot. */
export function CalendarCoverImage({
  calendar,
  alt = '',
  className,
  fallbackClassName = 'h-3 w-3 rounded-full',
  fallbackColor = calendar.color,
  inactive = false,
}: CalendarCoverImageProps) {
  const imageUrl = calendar.coverImageUrl || undefined;
  return (
    <CoverImageView
      key={imageUrl ?? 'no-cover'}
      imageUrl={imageUrl}
      alt={alt}
      className={className}
      fallbackClassName={fallbackClassName}
      fallbackColor={fallbackColor}
      inactive={inactive}
    />
  );
}

function CoverImageView({
  imageUrl,
  alt,
  className,
  fallbackClassName,
  fallbackColor,
  inactive,
}: {
  imageUrl: string | undefined;
  alt: string;
  className: string;
  fallbackClassName: string;
  fallbackColor: string;
  inactive: boolean;
}) {
  const [failed, setFailed] = useState(false);

  if (!imageUrl || failed) {
    return (
      <span
        aria-hidden="true"
        data-calendar-cover-fallback
        className={fallbackClassName}
        style={{
          backgroundColor: fallbackColor,
          opacity: inactive ? 0.3 : 1,
        }}
      />
    );
  }

  return (
    <img
      key={imageUrl}
      src={imageUrl}
      alt={alt}
      className={className}
      referrerPolicy="no-referrer"
      style={{ opacity: inactive ? 0.3 : 1 }}
      onError={() => setFailed(true)}
    />
  );
}
