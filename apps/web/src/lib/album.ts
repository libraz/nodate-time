import type { DateTime } from 'luxon';
import type { Locale } from '@/i18n';
import { fromISOInZone, nowInZone } from '@/lib/date-utils';

export interface AlbumPhoto {
  id: string;
  /** Sent as If-Match when the photo is edited or deleted. */
  etag: string;
  caption: string;
  imageUrl: string;
  /**
   * The grid-sized copy of the photo, absent when it has none. Photos uploaded
   * before thumbnails existed never get one, so this stays optional.
   */
  thumbnailUrl?: string;
  createdAt: string;
  takenAt: string;
  /** Public id of the event this photo belongs to, empty when it belongs to none. */
  eventId?: string;
  uploaderId: string;
  uploaderName: string;
  uploaderAvatarUrl?: string;
}

export interface AlbumDay {
  /** ISO date of the day, in the viewer's zone. */
  day: string;
  date: DateTime;
  photos: AlbumPhoto[];
}

/**
 * Splits photos into runs by the day they were taken. The server already
 * orders by capture time, so consecutive photos of one day stay together and
 * the grid keeps that order rather than re-sorting it.
 */
export function groupByDay(photos: AlbumPhoto[], zone: string): AlbumDay[] {
  const days: AlbumDay[] = [];
  for (const photo of photos) {
    const date = fromISOInZone(photo.takenAt, zone).startOf('day');
    const day = date.toISODate() ?? '';
    const last = days[days.length - 1];
    if (last && last.day === day) last.photos.push(photo);
    else days.push({ day, date, photos: [photo] });
  }
  return days;
}

/** A day heading: the year is left out for the current one, as a camera roll does. */
export function formatAlbumDay(date: DateTime, zone: string, locale: Locale): string {
  const sameYear = date.year === nowInZone(zone).year;
  return date.setLocale(locale).toLocaleString({
    ...(sameYear ? {} : { year: 'numeric' }),
    month: 'long',
    day: 'numeric',
    weekday: 'short',
  });
}

/** The viewer's title: the full date, with the capture time beneath it. */
export function formatAlbumMoment(
  iso: string,
  zone: string,
  locale: Locale,
): { date: string; time: string } {
  const dt = fromISOInZone(iso, zone).setLocale(locale);
  return {
    date: dt.toLocaleString({ year: 'numeric', month: 'long', day: 'numeric' }),
    time: dt.toLocaleString({ hour: '2-digit', minute: '2-digit' }),
  };
}
