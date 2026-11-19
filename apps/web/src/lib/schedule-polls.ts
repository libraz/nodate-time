import { api } from '@/lib/api';
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
}

export interface PollResponse {
  memberId: string;
  answers: PollAnswerEntry[];
}

export interface SchedulePoll {
  id: string;
  calendarId: string;
  title: string;
  timezone: string;
  status: PollStatus;
  version: number;
  reviewVersion: number;
  responseVersion: number;
  canManage: boolean;
  canTakeOver?: boolean;
  canRespond: boolean;
  slots: PollSlot[];
  members: PollMember[];
  myAnswers: PollAnswerEntry[];
  responses?: PollResponse[];
  respondedCount: number;
  memberCount: number;
  confirmedEventId?: string;
  selectedSlotId?: string;
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

export interface PollMemberListItem {
  id: string;
  name: string;
}

export interface SchedulePollCreateInput {
  title: string;
  timezone: string;
  memberIds: string[];
  slots: Array<{ startAt: string; endAt: string }>;
}

const calendarPollsPath = (calendarId: string) =>
  `/calendars/${encodeURIComponent(calendarId)}/schedule-polls`;
const pollPath = (calendarId: string, pollId: string) =>
  `${calendarPollsPath(calendarId)}/${encodeURIComponent(pollId)}`;

export const schedulePollApi = {
  async actions(signal?: AbortSignal): Promise<SchedulePollAction[]> {
    const result = await api.get<{ actions: SchedulePollAction[] }>(
      '/me/schedule-poll-actions',
      false,
      signal,
    );
    return result.actions ?? [];
  },

  async list(calendarId: string, signal?: AbortSignal): Promise<SchedulePoll[]> {
    const result = await api.get<{ polls: SchedulePoll[] }>(
      calendarPollsPath(calendarId),
      false,
      signal,
    );
    return result.polls ?? [];
  },

  get(calendarId: string, pollId: string, signal?: AbortSignal): Promise<SchedulePoll> {
    return api.get<SchedulePoll>(pollPath(calendarId, pollId), false, signal);
  },

  async members(calendarId: string, signal?: AbortSignal): Promise<PollMemberListItem[]> {
    const result = await api.get<{ members: PollMemberListItem[] }>(
      `${calendarPollsPath(calendarId)}/members`,
      false,
      signal,
    );
    return result.members ?? [];
  },

  availability(
    calendarId: string,
    input: SchedulePollAvailabilityInput,
    signal?: AbortSignal,
  ): Promise<{ slots: SchedulePollCandidate[] }> {
    return api.post<{ slots: SchedulePollCandidate[] }>(
      `${calendarPollsPath(calendarId)}/availability`,
      input,
      signal,
    );
  },

  create(calendarId: string, input: SchedulePollCreateInput): Promise<SchedulePoll> {
    return api.post<SchedulePoll>(calendarPollsPath(calendarId), input);
  },

  update(
    calendarId: string,
    pollId: string,
    input: {
      version: number;
      title?: string;
      memberIds?: string[];
      slots?: Array<{ id?: string; startAt: string; endAt: string }>;
      takeOver?: boolean;
    },
  ): Promise<SchedulePoll> {
    return api.patch<SchedulePoll>(pollPath(calendarId, pollId), input);
  },

  refresh(calendarId: string, pollId: string): Promise<SchedulePoll> {
    return api.post<SchedulePoll>(`${pollPath(calendarId, pollId)}/refresh-availability`);
  },

  respond(
    calendarId: string,
    pollId: string,
    input: { version: number; responseVersion: number; answers: PollAnswerEntry[] },
  ): Promise<SchedulePoll> {
    return api.put<SchedulePoll>(`${pollPath(calendarId, pollId)}/response`, input);
  },

  finalize(
    calendarId: string,
    pollId: string,
    input: {
      version: number;
      reviewVersion: number;
      slotId: string;
      acknowledged: boolean;
      ownerId?: string;
    },
  ): Promise<SchedulePoll> {
    return api.post<SchedulePoll>(`${pollPath(calendarId, pollId)}/finalize`, input);
  },

  cancel(calendarId: string, pollId: string, version: number): Promise<SchedulePoll> {
    return api.post<SchedulePoll>(`${pollPath(calendarId, pollId)}/cancel`, { version });
  },

  async getAvailabilitySharing(calendarId: string, signal?: AbortSignal): Promise<boolean> {
    const result = await api.get<{ enabled: boolean }>(
      `${calendarPollsPath(calendarId)}/availability-sharing`,
      false,
      signal,
    );
    return result.enabled;
  },

  async setAvailabilitySharing(calendarId: string, enabled: boolean): Promise<boolean> {
    const result = await api.put<{ enabled: boolean }>(
      `${calendarPollsPath(calendarId)}/availability-sharing`,
      { enabled },
    );
    return result.enabled;
  },

  event(calendarId: string, eventId: string, signal?: AbortSignal): Promise<CalendarEvent> {
    return api.get<CalendarEvent>(
      `/calendars/${encodeURIComponent(calendarId)}/events/${encodeURIComponent(eventId)}`,
      false,
      signal,
    );
  },
};
