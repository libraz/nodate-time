import type { CalendarEvent, Member } from '@/types/calendar';

/**
 * Which of an event's people a condition looks at. `involved` is whose event
 * it is or anyone listed on it, which is what "this person's events" means to
 * most readers; the other three name one role each.
 */
export type FilterField = 'involved' | 'owner' | 'creator' | 'participant';

export const FILTER_FIELDS: readonly FilterField[] = [
  'involved',
  'owner',
  'creator',
  'participant',
];

/** A chosen person, with the name kept so a chip still reads after they leave. */
export interface FilterPerson {
  id: string;
  name: string;
  avatarUrl?: string;
  /** Their layer colour on the first shown calendar they are on. */
  color?: string;
}

/**
 * One row of the filter. `exclude` turns "matches any of these people" into
 * "matches none of them". A row with nobody chosen is ignored.
 */
export interface FilterCondition {
  id: string;
  field: FilterField;
  exclude: boolean;
  people: FilterPerson[];
}

export interface EventFilter {
  match: 'all' | 'any';
  conditions: FilterCondition[];
}

export const EMPTY_FILTER: EventFilter = { match: 'all', conditions: [] };

/** Long enough that scanning a list for a name is slower than typing it. */
export const SEARCHABLE_FROM = 8;

/** The rows that take part: a row with nobody chosen says nothing yet. */
export function effectiveConditions(filter: EventFilter): FilterCondition[] {
  return filter.conditions.filter((c) => c.people.length > 0);
}

export function isFilterActive(filter: EventFilter): boolean {
  return effectiveConditions(filter).length > 0;
}

function hitsField(event: CalendarEvent, field: FilterField, ids: Set<string>): boolean {
  const owner = !!event.ownerId && ids.has(event.ownerId);
  const listed = (event.participantIds ?? []).some((id) => ids.has(id));
  switch (field) {
    case 'involved':
      return owner || listed;
    case 'owner':
      return owner;
    case 'creator':
      return !!event.creatorId && ids.has(event.creatorId);
    case 'participant':
      return listed;
  }
}

function matchesCondition(event: CalendarEvent, condition: FilterCondition): boolean {
  const hit = hitsField(event, condition.field, new Set(condition.people.map((p) => p.id)));
  return condition.exclude ? !hit : hit;
}

export function matchesFilter(event: CalendarEvent, filter: EventFilter): boolean {
  const rows = effectiveConditions(filter);
  if (rows.length === 0) return true;
  return filter.match === 'all'
    ? rows.every((c) => matchesCondition(event, c))
    : rows.some((c) => matchesCondition(event, c));
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function parsePerson(v: unknown): FilterPerson | null {
  if (!isRecord(v) || typeof v.id !== 'string' || typeof v.name !== 'string') return null;
  const person: FilterPerson = { id: v.id, name: v.name };
  for (const key of ['avatarUrl', 'color'] as const) {
    const value = v[key];
    if (value === undefined) continue;
    if (typeof value !== 'string') return null;
    if (value) person[key] = value;
  }
  return person;
}

function parseCondition(v: unknown): FilterCondition | null {
  if (!isRecord(v) || typeof v.id !== 'string' || typeof v.exclude !== 'boolean') return null;
  if (!FILTER_FIELDS.includes(v.field as FilterField) || !Array.isArray(v.people)) return null;
  const people: FilterPerson[] = [];
  for (const raw of v.people) {
    const p = parsePerson(raw);
    if (!p) return null;
    people.push(p);
  }
  return { id: v.id, field: v.field as FilterField, exclude: v.exclude, people };
}

/** Reads a stored filter; anything not of the expected shape reads as no filter. */
export function parseEventFilter(raw: unknown): EventFilter {
  if (!isRecord(raw) || (raw.match !== 'all' && raw.match !== 'any')) return EMPTY_FILTER;
  if (!Array.isArray(raw.conditions)) return EMPTY_FILTER;
  const conditions: FilterCondition[] = [];
  for (const c of raw.conditions) {
    const parsed = parseCondition(c);
    if (!parsed) return EMPTY_FILTER;
    conditions.push(parsed);
  }
  return { match: raw.match, conditions };
}

function toPerson(m: Pick<Member, 'id' | 'name' | 'avatarUrl' | 'color'>): FilterPerson {
  const person: FilterPerson = { id: m.id, name: m.name };
  if (m.avatarUrl) person.avatarUrl = m.avatarUrl;
  if (m.color) person.color = m.color;
  return person;
}

/**
 * The people a condition can name: everyone on a shown calendar, once each,
 * with the signed-in user first. Someone only on hidden calendars would match
 * nothing on screen, so they are not offered.
 */
export function filterCandidates(
  membersMap: Record<string, Member[]>,
  activeCalendarIds: readonly string[],
  meId: string | undefined,
): FilterPerson[] {
  const byId = new Map<string, FilterPerson>();
  for (const calId of activeCalendarIds) {
    for (const m of membersMap[calId] ?? []) {
      if (!byId.has(m.id)) byId.set(m.id, toPerson(m));
    }
  }
  const people = [...byId.values()];
  const meIndex = people.findIndex((p) => p.id === meId);
  if (meIndex > 0) people.unshift(...people.splice(meIndex, 1));
  return people;
}

/** A chosen person as they are now: the live name and avatar when still offered. */
export function resolvePerson(
  person: FilterPerson,
  candidates: readonly FilterPerson[],
): FilterPerson {
  return candidates.find((c) => c.id === person.id) ?? person;
}
