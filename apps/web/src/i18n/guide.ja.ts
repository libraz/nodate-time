/**
 * Onboarding, tip, empty-state and help copy. Kept apart from the main
 * dictionary because it is long-form prose; it is spread into `ja` so there is
 * still one key space.
 */
export const guideJa = {
  // First-run wizard
  'onboarding.label': 'はじめてガイド',
  'onboarding.skip': 'スキップ',
  'onboarding.back': '戻る',
  'onboarding.next': '次へ',
  'onboarding.finish': 'はじめる',
  'onboarding.step': '{current} / {total}',
  'onboarding.welcomeTitle': '{app} へようこそ',
  'onboarding.welcomeLead':
    '家族や仲間と、ひとつのカレンダーで予定を共有できます。はじめに使い方を 1 分でご案内します。',
  'onboarding.pointWrite': '予定を書き込む',
  'onboarding.pointWriteDesc': 'いつ・どこで・何をするかを書いておけます',
  'onboarding.pointShare': 'みんなで見る',
  'onboarding.pointShareDesc': '招待した人と、同じ予定を見られます',
  'onboarding.pointDecide': '日程を決める',
  'onboarding.pointDecideDesc': '集まれる日を、候補の中からみんなで選べます',
  'tour.menuTitle': 'メニュー',
  'tour.menuDesc': 'カレンダーの一覧や、招待・ヘルプはここから開けます。',
  'tour.calendarsTitle': 'カレンダーの一覧',
  'tour.calendarsDesc':
    'チェックを入れたカレンダーの予定が表示されます。「＋」で、共有する相手ごとに新しいカレンダーを作れます。',
  'tour.addTitle': '予定を追加する',
  'tour.addDesc': 'このボタンで予定を書き込めます。入れた予定は、押すといつでも直せます。',
  'tour.shareTitle': '一緒に使う人を招待する',
  'tour.shareDesc':
    'ここで招待リンクを作り、LINE やメールで送ります。受け取った人はこのカレンダーに参加できます。',
  'tour.helpTitle': '困ったときは',
  'tour.helpDesc': '使い方はいつでもここで調べられます。このガイドも、ここからもう一度見られます。',

  // Contextual tips
  'tip.label': 'ヒント',
  'tip.dismiss': 'わかった',
  'tip.learnMore': 'くわしく',
  'tip.guideTitle': 'はじめての方へ',
  'tip.guide': '使い方を 1 分でご案内',
  'tip.guideAction': 'ガイドを見る',
  'tip.members':
    '「権限」で、その人ができることが決まります。予定を書き込んでほしい人は「編集者」、見るだけの人は「閲覧者」にします。',
  'tip.share':
    '「招待リンク」は一緒に使う人に、「公開リンク」は見るだけの人に送ります。公開リンクはアカウントがなくても見られます。',
  'tip.memo':
    'メモには、日付に関係ない情報（持ち物リストや連絡事項など）を書いて、メンバーと共有できます。',
  'tip.event':
    '「誰の予定」で選んだ人の色で表示されます。「仮」や「空き」にしておくと、日程調整でその時間を候補にできます。',
  'tip.album': 'アルバムの写真は、このカレンダーのメンバー全員が見られます。',
  'tip.polls':
    '日程調整は、候補の日時をいくつか選んでメンバーに送り、都合のよい日をみんなで決める機能です。',

  // Empty states
  'empty.calendarsTitle': 'カレンダーがまだありません',
  'empty.calendarsDesc': 'カレンダーを作ると、予定を書き込めるようになります。',
  'empty.eventsTitle': '予定はありません',
  'empty.eventsDesc': '「＋」ボタンから予定を追加できます。',
  'empty.memosTitle': 'メモはまだありません',
  'empty.memosDesc': '伝えたいことや持ち物リストなどを書いて、メンバーと共有できます。',
  'empty.photosTitle': '写真はまだありません',
  'empty.photosDesc': '写真を追加すると、メンバー全員で見られます。',
  'empty.pollsTitle': '日程調整はまだありません',
  'empty.pollsDesc': '集まる日を決めたいときに、候補の日時を送ってみんなの都合を聞けます。',
  'empty.invitesTitle': '招待リンクはまだありません',
  'empty.invitesDesc': 'リンクを作って送ると、相手がこのカレンダーに参加できます。',
  'empty.activityTitle': 'アクティビティはまだありません',
  'empty.activityDesc': 'メンバーが予定を追加したり変更したりすると、ここに表示されます。',

  // Help page
  'help.title': 'ヘルプ',
  'help.lead': 'わからないことがあったら、ここで調べられます。',
  'help.open': 'ヘルプを開く',
  'help.openTopic': '「{topic}」のヘルプを開く',
  'help.contents': '目次',
  'help.replayGuide': 'はじめてガイドをもう一度見る',
  'help.resetTips': 'ヒントをもう一度表示する',
  'help.tipsReset': 'ヒントをもう一度表示します',

  'help.startTitle': 'はじめに',
  'help.startLead':
    'カレンダーを作る、予定を入れる、一緒に使う人を招待する。この 3 つで使い始められます。',
  'help.startStep1': 'メニューの「新しいカレンダーを作る」で、カレンダーを作ります。',
  'help.startStep2': '「＋」ボタンで、予定を追加します。',
  'help.startStep3':
    '「メンバー」の「追加」でワークスペースの人を選びます。ほかの人には「共有」で招待リンクを送ります。',

  'help.eventsTitle': '予定',
  'help.eventsAdd': '予定を追加する',
  'help.eventsAddDesc':
    '「＋」ボタンを押します（パソコンでは画面右上、スマホでは右下）。パソコンでは日付をダブルクリックしても追加できます。スマホでは日付を選んでから「＋」を押すと、その日の予定になります。',
  'help.eventsEdit': '予定を直す・消す',
  'help.eventsEditDesc':
    '予定を押すと、内容の確認と編集ができます。消すときは、編集画面の「削除」を押します。',
  'help.eventsRepeat': '繰り返しの予定',
  'help.eventsRepeatDesc':
    '「繰り返し」で、毎週・毎月などを選べます。繰り返しの予定を直すときは、「この予定のみ」か「シリーズ全体」かを選べます。',
  'help.eventsWhose': '誰の予定か',
  'help.eventsWhoseDesc':
    '共有カレンダーでは、予定ごとに「誰の予定」かを選べます。予定はその人の色で表示されます。家族の通院や、担当しているメンバーの予定など、自分以外の人の予定も代わりに入れられます。',
  'help.eventsPeople': '参加者',
  'help.eventsPeopleDesc':
    '一緒に参加する人を選びます。選ばれた人は、予定を開くと「参加・未定・不参加」で返事ができます。日程調整では、参加者の空き時間にもこの予定が数えられます（「不参加」と答えた人を除く）。',
  'help.eventsShowAs': '空き状況（予定あり・仮・空き・不在）',
  'help.eventsShowAsDesc':
    '日程調整で空いている時間を探すときの扱いを決めます。「予定あり」と「不在」はその時間を埋めます。「仮」は「調整できるかも」として残し、「空き」は予定を入れても空いているものとして扱います。まだ確定していない予定は「仮」、メモ代わりの予定は「空き」にしておくと便利です。',
  'help.eventsAllDay': '終日の予定',
  'help.eventsAllDayDesc': '時間を決めない予定（旅行や誕生日など）は「終日」にします。',
  'help.eventsVisibility': '公開範囲',
  'help.eventsVisibilityDesc':
    '「時間のみ」にすると、他のメンバーには予定があることだけが見え、内容は見えません。',

  'help.calendarsTitle': 'カレンダーとメンバー',
  'help.calendarsWhat': 'カレンダー',
  'help.calendarsWhatDesc':
    '予定を入れておく入れ物です。共有したい相手ごとに分けて作れます。左のメニューのチェックで、表示する・しないを切り替えられます。',
  'help.calendarsColor': '予定の色',
  'help.calendarsColorDesc':
    '予定は「誰の予定」で選んだ人の色で表示されます。色は「メンバー」の画面で変えられます。',
  'help.rolesTitle': '権限（できること）',
  'help.roleOwnerDesc': 'すべての操作ができます。カレンダーを削除できるのはオーナーだけです。',
  'help.roleManagerDesc': 'メンバーの追加や、権限の変更ができます。',
  'help.roleEditorDesc': '予定・メモ・写真を書き込めます。',
  'help.roleViewerDesc': '見ることだけができます。',

  'help.inviteTitle': '招待と共有',
  'help.directAdd': 'ワークスペースから追加',
  'help.directAddDesc':
    '同じワークスペースの人は、「メンバー」の「追加」から選ぶだけで加えられます。何人かまとめて選び、編集者か閲覧者かを決めて追加します。リンクを送る必要はありません。',
  'help.inviteLink': '招待リンク',
  'help.inviteLinkDesc':
    '一緒に使う人に送るリンクです。開いた人はアカウントを作って、カレンダーに参加します。有効期限や使える人数を決められます。',
  'help.publicLink': '公開リンク',
  'help.publicLinkDesc':
    '見るだけのリンクです。リンクを知っている人なら、アカウントがなくても誰でも見られます。知られたくない予定があるカレンダーでは作らないでください。',
  'help.stopSharing': '共有をやめる',
  'help.stopSharingDesc': 'どちらのリンクも「無効にする」で、いつでも使えなくできます。',

  'help.pollsTitle': '日程調整',
  'help.pollsLead': '集まる日を決めたいときに使います。',
  'help.pollsStep1': '画面上の「日程調整」を開き、「日程調整を作成」を押します。',
  'help.pollsStep2': 'カレンダーと、都合を聞きたいメンバーを選びます。',
  'help.pollsStep3': '候補の日時を選んで送ります。メンバーの空き状況も確認できます。',
  'help.pollsStep4': '回答が集まったら、日にちを決めます。',

  'help.memosTitle': 'メモとアルバム',
  'help.memos': 'メモ',
  'help.memosDesc':
    '日付に関係ない情報（持ち物リストや連絡事項など）を、カレンダーのメンバーと共有できます。',
  'help.album': 'アルバム',
  'help.albumDesc': '写真をメンバーと共有できます。写真は予定にひもづけることもできます。',

  'help.viewsTitle': '表示の切り替え',
  'help.viewsModes': 'マンスリー・ウィークリー・リスト・年表示',
  'help.viewsModesDesc':
    '画面上の表示切り替えから選べます。ウィークリーでは、時間ごとの予定が見やすくなります。',
  'help.viewsToday': '今日に戻る',
  'help.viewsTodayDesc': '「今日」ボタンを押すと、今日の日付に戻ります。',
  'help.viewsSearch': '予定を探す',
  'help.viewsSearchDesc': '虫めがねのボタンから、予定の名前で探せます。',
  'help.viewsAppearance': '見た目と言語',
  'help.viewsAppearanceDesc': '「外観」や「設定」から、テーマや明るさ、言語を変えられます。',

  'help.faqTitle': 'よくある質問',
  'help.faqNotVisibleQ': '招待した人に予定が見えません',
  'help.faqNotVisibleA':
    '相手が招待リンクから参加できたか、「メンバー」で確認してください。参加しているのに見えない場合は、相手の画面の左のメニューで、そのカレンダーにチェックが入っているか確認してください。',
  'help.faqMissingQ': '予定が消えてしまいました',
  'help.faqMissingA':
    '左のメニューでカレンダーのチェックが外れていると、そのカレンダーの予定は表示されません。「アクティビティ」では、誰がいつ予定を変更したかを確認できます。',
  'help.faqDevicesQ': '別のスマホやパソコンでも使えますか',
  'help.faqDevicesA': 'はい。同じアカウントでログインすれば、どの端末でも同じ予定を見られます。',
  'help.faqLeaveQ': 'カレンダーから抜けたい',
  'help.faqLeaveA':
    '「メンバー」の画面の「カレンダーから退出」で抜けられます。あなたが入れた予定は、他のメンバーには残ります。',
} as const;
