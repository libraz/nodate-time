import type { guideJa } from './guide.ja';

export const guideEn: Record<keyof typeof guideJa, string> = {
  'onboarding.label': 'Getting started',
  'onboarding.skip': 'Skip',
  'onboarding.back': 'Back',
  'onboarding.next': 'Next',
  'onboarding.finish': 'Get started',
  'onboarding.step': '{current} / {total}',
  'onboarding.welcomeTitle': 'Welcome to {app}',
  'onboarding.welcomeLead':
    'Share plans with family and friends on one calendar. Here is a one-minute tour to get you going.',
  'onboarding.pointWrite': 'Write down plans',
  'onboarding.pointWriteDesc': 'Keep track of when, where and what',
  'onboarding.pointShare': 'See them together',
  'onboarding.pointShareDesc': 'Everyone you invite sees the same plans',
  'onboarding.pointDecide': 'Pick a date',
  'onboarding.pointDecideDesc': 'Choose a day that works for everyone',
  'tour.menuTitle': 'Menu',
  'tour.menuDesc': 'Your calendars, invitations and help all open from here.',
  'tour.calendarsTitle': 'Your calendars',
  'tour.calendarsDesc':
    'Plans from ticked calendars are shown. Use "+" to make a new calendar for each group you share with.',
  'tour.addTitle': 'Add a plan',
  'tour.addDesc': 'Write down a plan with this button. Press any plan later to change it.',
  'tour.shareTitle': 'Invite the people you plan with',
  'tour.shareDesc':
    'Make an invite link here and send it in a message or an email. Whoever opens it can join this calendar.',
  'tour.helpTitle': 'When you are stuck',
  'tour.helpDesc':
    'Look up how things work here at any time. You can replay this tour from here, too.',

  'tip.label': 'Tip',
  'tip.dismiss': 'Got it',
  'tip.learnMore': 'Learn more',
  'tip.guideTitle': 'New here?',
  'tip.guide': 'Take a one-minute tour of the basics.',
  'tip.guideAction': 'Start the tour',
  'tip.members':
    'The role decides what someone can do. Make people who add plans "Editors", and people who only look "Viewers".',
  'tip.share':
    'Send an invite link to people who plan with you, and a public link to people who only look. A public link works without an account.',
  'tip.memo':
    "Memos hold things that are not tied to a date, like packing lists or notices, shared with the calendar's members.",
  'tip.event':
    'A plan is shown in the colour of the person picked under "Whose plan". Mark it "Tentative" or "Free" to keep that time open when scheduling.',
  'tip.album': 'Every member of this calendar can see the photos in its album.',

  'empty.calendarsTitle': 'No calendars yet',
  'empty.calendarsDesc': 'Create a calendar to start adding plans.',
  'empty.eventsTitle': 'No plans',
  'empty.eventsDesc': 'Add one with the "+" button.',
  'empty.memosTitle': 'No memos yet',
  'empty.memosDesc': 'Write down notices or packing lists and share them with members.',
  'empty.photosTitle': 'No photos yet',
  'empty.photosDesc': 'Photos you add are visible to every member.',
  'empty.pollsTitle': 'No schedule polls yet',
  'empty.pollsDesc': 'When you need to pick a day, send candidate times and ask who can make it.',
  'empty.invitesTitle': 'No invite links yet',
  'empty.invitesDesc': 'Create a link and send it so others can join this calendar.',
  'empty.activityTitle': 'No activity yet',
  'empty.activityDesc': 'When members add or change plans, it shows up here.',

  'help.title': 'Help',
  'help.lead': 'Look things up here whenever you are unsure.',
  'help.open': 'Open help',
  'help.openTopic': 'Open help on "{topic}"',
  'help.contents': 'Contents',
  'help.replayGuide': 'Replay the getting-started tour',
  'help.resetTips': 'Show tips again',
  'help.tipsReset': 'Tips will be shown again',

  'help.startTitle': 'Getting started',
  'help.startLead':
    'Create a calendar, add plans, and invite the people you plan with. That is all it takes.',
  'help.startStep1': 'Create a calendar with "New calendar" in the menu.',
  'help.startStep2': 'Add a plan with the "+" button.',
  'help.startStep3':
    'Pick people from your workspace with "Add" under "Members", or send anyone else an invite link from "Share".',

  'help.eventsTitle': 'Plans',
  'help.eventsAdd': 'Adding a plan',
  'help.eventsAddDesc':
    'Press the "+" button (top right on a computer, bottom right on a phone). On a computer you can also double-click a date. On a phone, pick a date first and then press "+".',
  'help.eventsEdit': 'Changing or deleting a plan',
  'help.eventsEditDesc':
    'Press a plan to see and edit it. To remove it, press "Delete" in the edit screen.',
  'help.eventsRepeat': 'Repeating plans',
  'help.eventsRepeatDesc':
    'Use "Repeat" to choose weekly, monthly and so on. When you change a repeating plan, choose "This event only" or "All events in the series".',
  'help.eventsWhose': 'Whose plan',
  'help.eventsWhoseDesc':
    'On a shared calendar each plan belongs to someone, picked under "Whose plan", and is shown in their colour. You can add plans for other people too, such as a family member\'s appointment or a plan for someone you look after.',
  'help.eventsPeople': 'Participants',
  'help.eventsPeopleDesc':
    'Pick who else takes part. They can answer "Going", "Maybe" or "Not going" when they open the plan. When scheduling, the plan counts against their free time too, unless they said they are not going.',
  'help.eventsShowAs': 'Availability (Busy, Tentative, Free, Out of office)',
  'help.eventsShowAsDesc':
    'Decides how scheduling treats the time. "Busy" and "Out of office" take it. "Tentative" leaves it as "might be able to move", and "Free" keeps it open even with a plan in it. Use "Tentative" for plans not yet settled and "Free" for reminders.',
  'help.eventsAllDay': 'All-day plans',
  'help.eventsAllDayDesc': 'Mark plans without a set time, like trips or birthdays, as "All day".',
  'help.eventsVisibility': 'Who sees the details',
  'help.eventsVisibilityDesc':
    'With "Time only", other members see that you are busy but not what the plan is.',

  'help.calendarsTitle': 'Calendars and members',
  'help.calendarsWhat': 'Calendars',
  'help.calendarsWhatDesc':
    'A calendar holds plans. Make one for each group you share with. Tick or untick it in the left menu to show or hide it.',
  'help.calendarsColor': 'Plan colours',
  'help.calendarsColorDesc':
    'A plan is shown in the colour of the person picked under "Whose plan". Change colours under "Members".',
  'help.rolesTitle': 'Roles (what people can do)',
  'help.roleOwnerDesc': 'Can do everything. Only owners can delete the calendar.',
  'help.roleManagerDesc': 'Can add members and change their roles.',
  'help.roleEditorDesc': 'Can add plans, memos and photos.',
  'help.roleViewerDesc': 'Can only look.',

  'help.inviteTitle': 'Inviting and sharing',
  'help.directAdd': 'Add from your workspace',
  'help.directAddDesc':
    'People in the same workspace can be added from "Add" under "Members" without a link. Choose several at once, pick editor or viewer, and add them.',
  'help.inviteLink': 'Invite link',
  'help.inviteLinkDesc':
    'A link for people who plan with you. Whoever opens it creates an account and joins the calendar. You can set how long it lasts and how many people can use it.',
  'help.publicLink': 'Public link',
  'help.publicLinkDesc':
    'A view-only link. Anyone who has it can see the calendar without an account. Do not create one for a calendar with plans you would not want others to see.',
  'help.stopSharing': 'Stop sharing',
  'help.stopSharingDesc': 'Either link can be switched off at any time.',

  'help.pollsTitle': 'Schedule polls',
  'help.pollsLead': 'Use these when you need to pick a day to meet.',
  'help.pollsStep1': 'Open "Scheduling" at the top and press "Create a schedule poll".',
  'help.pollsStep2': 'Choose the calendar and the members to ask.',
  'help.pollsStep3': 'Pick candidate times and send them. You can check who is free, too.',
  'help.pollsStep4': 'Once the answers are in, settle on a date.',

  'help.memosTitle': 'Memos and album',
  'help.memos': 'Memos',
  'help.memosDesc':
    "Share things that are not tied to a date, like packing lists or notices, with the calendar's members.",
  'help.album': 'Album',
  'help.albumDesc': 'Share photos with members. A photo can also be linked to a plan.',

  'help.viewsTitle': 'Changing the view',
  'help.viewsModes': 'Monthly, weekly, list and year',
  'help.viewsModesDesc':
    'Pick one from the view switch at the top. The weekly view makes times of day easy to read.',
  'help.viewsToday': 'Back to today',
  'help.viewsTodayDesc': 'Press "Today" to jump back to the current date.',
  'help.viewsSearch': 'Finding a plan',
  'help.viewsSearchDesc': 'Use the magnifying-glass button to search plans by name.',
  'help.viewsAppearance': 'Look and language',
  'help.viewsAppearanceDesc':
    'Change the theme, light or dark, and language from "Appearance" or "Settings".',

  'help.faqTitle': 'Frequently asked questions',
  'help.faqNotVisibleQ': 'The person I invited cannot see my plans',
  'help.faqNotVisibleA':
    'Check under "Members" that they joined through the invite link. If they joined but still see nothing, ask them to tick the calendar in their left menu.',
  'help.faqMissingQ': 'A plan has disappeared',
  'help.faqMissingA':
    'Plans from a calendar that is unticked in the left menu are hidden. "Activity" shows who changed which plan and when.',
  'help.faqDevicesQ': 'Can I use it on another phone or computer?',
  'help.faqDevicesA':
    'Yes. Sign in with the same account and you will see the same plans anywhere.',
  'help.faqLeaveQ': 'I want to leave a calendar',
  'help.faqLeaveA':
    'Use "Leave calendar" under "Members". Plans you added stay for the other members.',
  'help.faqCalendarSyncQ': 'Can I see my plans in the iPhone or Mac Calendar app?',
  'help.faqCalendarSyncA':
    'Yes. On an iPhone, iPad or Mac, open Settings → "Calendar app sync" in the browser on that device, press the button such as "Add this iPhone", and install the profile that appears. To set up an iPhone or iPad from a computer, choose "iPhone or iPad" to show a QR code and scan it with the camera. On Android and other devices, choose "Android or other" and enter the server, username and password shown into a CalDAV account in your calendar app. To stop syncing, press "Stop syncing" next to the device in the same list, and remove the profile (or account) on the device as well. Signing out or changing your password does not stop sync. Labels, checklists, attachments, participants and comments are not shown in the Calendar app.',
};
