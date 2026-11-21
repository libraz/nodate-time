import type { components } from '@/lib/api-schema';
import { client } from '@/lib/client';
import type { CalendarEvent } from '@/types/calendar';

export type PollAnswer = 'yes' | 'maybe' | 'no';
export type PollAvailability = 'open' | 'soft' | 'hard';
export type PollStatus = 'open' | 'finalized' | 'cancelled';
export type PollActionKind = 'answer' | 'finalize' | 'add-slots';

export interface PollSlot {
  id: string;
  startAt: string;
  endAt: string;
  availability: PollAvailability;
  active: boolean;
}

export interface PollAnswerEntry {
  slotId: string;
  value: PollAnswer;
}

export interface PollMember {
  id: string;
  name: string;
  avatarUrl?: string;
}

export interface PollResponse {
  memberId: string;
  answers: PollAnswerEntry[];
}

export interface SchedulePoll {
  id: string;
  /** Sent as If-Match on every write to the poll. */
  etag: string;
  calendarId: string;
  title: string;
  timezone: string;
  status: PollStatus;
  canManage: boolean;
  canTakeOver?: boolean;
  canRespond: boolean;
  slots: PollSlot[];
  /** Absent for callers who may not manage the poll. */
  members?: PollMember[];
  myAnswers: PollAnswerEntry[];
  responses?: PollResponse[];
  respondedCount: number;
  memberCount: number;
  confirmedEventId?: string;
  selectedSlotId?: string;
}

/** The caller's own answers with the etag that PUT .../response must send. */
export interface PollMyResponse {
  etag: string;
  answers: PollAnswerEntry[];
}

export interface SchedulePollAction {
  calendarId: string;
  calendarName: string;
  pollId: string;
  title: string;
  kind: PollActionKind;
}

export interface SchedulePollCandidate {
  startAt: string;
  endAt: string;
  availability: PollAvailability;
  active: boolean;
}

export interface SchedulePollAvailabilityInput {
  timezone: string;
  fromDate: string;
  toDate: string;
  startTime: string;
  endTime: string;
  durationMinutes: number;
  memberIds: string[];
  slots?: Array<{ startAt: string; endAt: string }>;
}

export interface SchedulePollCreateInput {
  title: string;
  timezone: string;
  memberIds: string[];
  slots: Array<{ startAt: string; endAt: string }>;
}

type PollWire = Omit<components['schemas']['PollDetailResponse'], '$schema'>;

/** Collapses the wire's nullable lists into the arrays the UI iterates. */
function toPoll(wire: PollWire): SchedulePoll {
  const { members, myAnswers, responses, slots, ...rest } = wire;
  return {
    ...rest,
    slots: slots ?? [],
    myAnswers: myAnswers ?? [],
    ...(members ? { members } : {}),
    ...(responses
      ? { responses: responses.map((r) => ({ memberId: r.memberId, answers: r.answers ?? [] })) }
      : {}),
  };
}

function present<T>(data: T | undefined): T {
  if (data === undefined) throw new Error('Empty response body');
  return data;
}

/** Leaves `signal` out when absent, as exactOptionalPropertyTypes requires. */
const withSignal = (signal?: AbortSignal) => (signal ? { signal } : {});

const pathOf = (calendarId: string, pollId: string) => ({ calendarId, pollId });

export const schedulePollApi = {
  async actions(signal?: AbortSignal): Promise<SchedulePollAction[]> {
    const { data } = await client.GET('/user/schedule-poll-actions', { ...withSignal(signal) });
    return present(data) ?? [];
  },

  async list(calendarId: string, signal?: AbortSignal): Promise<SchedulePoll[]> {
    const { data } = await client.GET('/calendars/{calendarId}/schedule-polls', {
      params: { path: { calendarId } },
      ...withSignal(signal),
    });
    return (present(data) ?? []).map(toPoll);
  },

  async get(calendarId: string, pollId: string, signal?: AbortSignal): Promise<SchedulePoll> {
    const { data } = await client.GET('/calendars/{calendarId}/schedule-polls/{pollId}', {
      params: { path: pathOf(calendarId, pollId) },
      ...withSignal(signal),
    });
    return toPoll(present(data));
  },

  async getResponse(
    calendarId: string,
    pollId: string,
    signal?: AbortSignal,
  ): Promise<PollMyResponse> {
    const { data } = await client.GET('/calendars/{calendarId}/schedule-polls/{pollId}/response', {
      params: { path: pathOf(calendarId, pollId) },
      ...withSignal(signal),
    });
    const body = present(data);
    return { etag: body.etag, answers: body.answers ?? [] };
  },

  async availability(
    calendarId: string,
    input: SchedulePollAvailabilityInput,
    signal?: AbortSignal,
  ): Promise<SchedulePollCandidate[]> {
    const { data } = await client.POST('/calendars/{calendarId}/schedule-polls/availability', {
      params: { path: { calendarId } },
      body: input,
      ...withSignal(signal),
    });
    return present(data) ?? [];
  },

  async create(calendarId: string, input: SchedulePollCreateInput): Promise<SchedulePoll> {
    const { data } = await client.POST('/calendars/{calendarId}/schedule-polls', {
      params: { path: { calendarId } },
      body: input,
    });
    return toPoll(present(data));
  },

  async update(
    calendarId: string,
    pollId: string,
    etag: string,
    input: {
      title?: string;
      memberIds?: string[];
      slots?: Array<{ id?: string; startAt: string; endAt: string }>;
      takeOver?: boolean;
    },
  ): Promise<SchedulePoll> {
    const { data } = await client.PATCH('/calendars/{calendarId}/schedule-polls/{pollId}', {
      params: { path: pathOf(calendarId, pollId), header: { 'If-Match': etag } },
      body: input,
    });
    return toPoll(present(data));
  },

  async refresh(calendarId: string, pollId: string): Promise<SchedulePoll> {
    const { data } = await client.POST(
      '/calendars/{calendarId}/schedule-polls/{pollId}/refresh-availability',
      { params: { path: pathOf(calendarId, pollId) } },
    );
    return toPoll(present(data));
  },

  async respond(
    calendarId: string,
    pollId: string,
    answerEtag: string,
    answers: PollAnswerEntry[],
  ): Promise<PollMyResponse> {
    const { data } = await client.PUT('/calendars/{calendarId}/schedule-polls/{pollId}/response', {
      params: { path: pathOf(calendarId, pollId), header: { 'If-Match': answerEtag } },
      body: { answers },
    });
    const body = present(data);
    return { etag: body.etag, answers: body.answers ?? [] };
  },

  async finalize(
    calendarId: string,
    pollId: string,
    etag: string,
    input: { slotId: string; acknowledged: boolean; ownerId?: string },
  ): Promise<SchedulePoll> {
    const { data } = await client.POST('/calendars/{calendarId}/schedule-polls/{pollId}/finalize', {
      params: { path: pathOf(calendarId, pollId), header: { 'If-Match': etag } },
      body: input,
    });
    return toPoll(present(data));
  },

  async cancel(calendarId: string, pollId: string, etag: string): Promise<SchedulePoll> {
    const { data } = await client.POST('/calendars/{calendarId}/schedule-polls/{pollId}/cancel', {
      params: { path: pathOf(calendarId, pollId), header: { 'If-Match': etag } },
    });
    return toPoll(present(data));
  },

  async getAvailabilitySharing(calendarId: string, signal?: AbortSignal): Promise<boolean> {
    const { data } = await client.GET(
      '/calendars/{calendarId}/schedule-polls/availability-sharing',
      {
        params: { path: { calendarId } },
        ...withSignal(signal),
      },
    );
    return present(data).enabled;
  },

  async setAvailabilitySharing(calendarId: string, enabled: boolean): Promise<boolean> {
    const { data } = await client.PUT(
      '/calendars/{calendarId}/schedule-polls/availability-sharing',
      {
        params: { path: { calendarId } },
        body: { enabled },
      },
    );
    return present(data).enabled;
  },

  async event(calendarId: string, eventId: string, signal?: AbortSignal): Promise<CalendarEvent> {
    const { data } = await client.GET('/calendars/{calendarId}/events/{eventId}', {
      params: { path: { calendarId, eventId } },
      ...withSignal(signal),
    });
    return present(data) as CalendarEvent;
  },
};
