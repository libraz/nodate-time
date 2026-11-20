import type { TranslationKey } from '@/i18n';

export type HelpTopic =
  | 'start'
  | 'events'
  | 'calendars'
  | 'invite'
  | 'polls'
  | 'memos'
  | 'views'
  | 'faq';

/** One help section's body, rendered in order. */
export type HelpBlock =
  | { kind: 'lead'; text: TranslationKey }
  | { kind: 'steps'; items: TranslationKey[] }
  | {
      kind: 'terms';
      heading?: TranslationKey;
      items: { term: TranslationKey; desc: TranslationKey }[];
    }
  | { kind: 'faq'; items: { q: TranslationKey; a: TranslationKey }[] };

export interface HelpSection {
  id: HelpTopic;
  title: TranslationKey;
  blocks: HelpBlock[];
}

/** The help page, top to bottom; the ids double as URL fragments. */
export const HELP_SECTIONS: HelpSection[] = [
  {
    id: 'start',
    title: 'help.startTitle',
    blocks: [
      { kind: 'lead', text: 'help.startLead' },
      { kind: 'steps', items: ['help.startStep1', 'help.startStep2', 'help.startStep3'] },
    ],
  },
  {
    id: 'events',
    title: 'help.eventsTitle',
    blocks: [
      {
        kind: 'terms',
        items: [
          { term: 'help.eventsAdd', desc: 'help.eventsAddDesc' },
          { term: 'help.eventsEdit', desc: 'help.eventsEditDesc' },
          { term: 'help.eventsRepeat', desc: 'help.eventsRepeatDesc' },
          { term: 'help.eventsAllDay', desc: 'help.eventsAllDayDesc' },
          { term: 'help.eventsVisibility', desc: 'help.eventsVisibilityDesc' },
        ],
      },
    ],
  },
  {
    id: 'calendars',
    title: 'help.calendarsTitle',
    blocks: [
      {
        kind: 'terms',
        items: [
          { term: 'help.calendarsWhat', desc: 'help.calendarsWhatDesc' },
          { term: 'help.calendarsColor', desc: 'help.calendarsColorDesc' },
        ],
      },
      {
        kind: 'terms',
        heading: 'help.rolesTitle',
        items: [
          { term: 'members.roleOwner', desc: 'help.roleOwnerDesc' },
          { term: 'members.roleManager', desc: 'help.roleManagerDesc' },
          { term: 'members.roleEditor', desc: 'help.roleEditorDesc' },
          { term: 'members.roleViewer', desc: 'help.roleViewerDesc' },
        ],
      },
    ],
  },
  {
    id: 'invite',
    title: 'help.inviteTitle',
    blocks: [
      {
        kind: 'terms',
        items: [
          { term: 'help.inviteLink', desc: 'help.inviteLinkDesc' },
          { term: 'help.publicLink', desc: 'help.publicLinkDesc' },
          { term: 'help.stopSharing', desc: 'help.stopSharingDesc' },
        ],
      },
    ],
  },
  {
    id: 'polls',
    title: 'help.pollsTitle',
    blocks: [
      { kind: 'lead', text: 'help.pollsLead' },
      {
        kind: 'steps',
        items: ['help.pollsStep1', 'help.pollsStep2', 'help.pollsStep3', 'help.pollsStep4'],
      },
    ],
  },
  {
    id: 'memos',
    title: 'help.memosTitle',
    blocks: [
      {
        kind: 'terms',
        items: [
          { term: 'help.memos', desc: 'help.memosDesc' },
          { term: 'help.album', desc: 'help.albumDesc' },
        ],
      },
    ],
  },
  {
    id: 'views',
    title: 'help.viewsTitle',
    blocks: [
      {
        kind: 'terms',
        items: [
          { term: 'help.viewsModes', desc: 'help.viewsModesDesc' },
          { term: 'help.viewsToday', desc: 'help.viewsTodayDesc' },
          { term: 'help.viewsSearch', desc: 'help.viewsSearchDesc' },
          { term: 'help.viewsAppearance', desc: 'help.viewsAppearanceDesc' },
        ],
      },
    ],
  },
  {
    id: 'faq',
    title: 'help.faqTitle',
    blocks: [
      {
        kind: 'faq',
        items: [
          { q: 'help.faqNotVisibleQ', a: 'help.faqNotVisibleA' },
          { q: 'help.faqMissingQ', a: 'help.faqMissingA' },
          { q: 'help.faqDevicesQ', a: 'help.faqDevicesA' },
          { q: 'help.faqLeaveQ', a: 'help.faqLeaveA' },
        ],
      },
    ],
  },
];
