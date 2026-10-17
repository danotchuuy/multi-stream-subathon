// Mirrors the JSON shapes produced by internal/subathon (Timer, Manager)
// and internal/server (auth handlers) on the Go backend.

/** Platforms an account identity can be linked to (login or link). */
export type AuthPlatform = 'twitch' | 'kick' | 'youtube'

export type Platform =
  | 'kick'
  | 'youtube'
  | 'twitch'
  | 'streamelements'
  | 'throne'
  | 'manual'

/** Platforms reward/money rates are configured for — every Platform
 * except 'manual', which specifies its own seconds/dollars directly
 * rather than looking one up (see RewardRules/MoneyRules). */
export type RewardPlatform =
  | 'twitch'
  | 'kick'
  | 'youtube'
  | 'streamelements'
  | 'throne'

export type EventType = 'sub' | 'resub' | 'gifted_sub' | 'donation' | 'bits' | 'gems' | 'manual'

export interface SubathonEvent {
  id: string
  timerId: string
  platform: Platform
  type: EventType
  username: string
  secondsAdded: number
  /** Dollars this event counted toward the timer's money goal, per
   * MoneyRules — independent of secondsAdded. */
  moneyAdded?: number
  amount?: number
  occurred: string
  /** Display name of the dashboard user who entered this by hand. */
  addedBy?: string
}

export interface Snapshot {
  id: string
  name: string
  running: boolean
  startedAt?: string
  endsAt: string
  remainingSecs: number
  totalAddedSecs: number
  recentEvents: SubathonEvent[]
  /** Twitch channel this timer is configured to watch, if any. */
  twitchChannel?: string
  /** Kick channel this timer is configured to watch, if any. */
  kickChannel?: string
  /** YouTube channel this timer is configured to watch, if any —
   * youtubeChannelId is the picker-matching ID (see
   * GET /api/youtube/channels), youtubeChannel its display title. */
  youtubeChannel?: string
  youtubeChannelId?: string
  /** Running total of every event's moneyAdded. */
  totalMoneyRaised: number
  /** Configured dollar goal to raise toward; absent/0 means none set. */
  moneyGoal?: number
  /** While true, new contributions are still recorded (and still count
   * toward totalMoneyRaised) but no longer extend the clock. */
  locked: boolean
  /** While true, the public overlay renders nothing instead of the
   * clock. The dashboard always shows it regardless. */
  hidden: boolean
  /** Bumps whenever the money milestones are saved — see GoalsOverlay. */
  milestonesVersion?: number
  /** Bumps whenever the countdown alert list is saved — see Overlay's
   * useMilestoneAlertSounds. */
  alertsVersion?: number
  /** Bumps whenever a scheduled happy hour is added, cancelled, or
   * starts — see the dashboard's ScheduledHappyHours. The list itself is
   * owner/moderator-only, not part of this public snapshot. */
  happyHourScheduleVersion?: number
  /** While true, this timer no longer receives live platform events
   * (Twitch/Kick/StreamElements) or responds to any "!timer ..." chat
   * command — only reachable from the dashboard, never chat. Manual
   * events and every other dashboard control still work regardless. */
  ended: boolean
  /** While true, a "!timer hh <duration> [multiplier]"/dashboard "Start
   * Happy Hour" boost has every real contribution's time value multiplied
   * by boostMultiplier; boostEndsAt (ISO timestamp) is when it ends. False
   * (and boostEndsAt absent) once expired, not just when none has ever
   * been started. */
  boostActive: boolean
  boostEndsAt?: string
  boostMultiplier?: number
  /** Colors for the public overlays' pills. Always fully populated
   * (server fills in defaults), never partial. */
  overlayColors: OverlayColors

  /** Running totals of this timer's three "gift categories", for the
   * overlay's optional rotating stat list (see statsRotationEnabled).
   * subsGiven counts subs plus gifted subs (a 5-sub gift counts as 5);
   * bitsGiven counts bits/Kicks contributed; donationsGiven counts the
   * number of tip/donation events, not their dollar total (which
   * totalMoneyRaised already covers). */
  subsGiven: number
  bitsGiven: number
  donationsGiven: number

  /** Whether the main overlay's rotating stat list (subs/bits-Kicks/
   * donations, far left of the timer/money pills) is turned on. Off by
   * default. */
  statsRotationEnabled: boolean

  /** Whether the main overlay plays a "timer explodes" finale animation
   * once remainingSecs reaches 0, instead of just sitting at 00:00:00.
   * Off by default. */
  explosionEnabled: boolean

  /** Hex color (e.g. "#ff8c00") the finale's flash/ring/shards/embers
   * are tinted. Always populated (server fills in a default), never
   * empty. */
  explosionColor: string

  /** The main overlay's animation while boostActive (a Happy Hour).
   * Always fully populated (server fills in defaults), never partial. */
  happyHourStyle: HappyHourStyle

  /** Master on/off switch for the main overlay's countdown alerts
   * (client-side — see lib/alertSound.ts): while off, none of the
   * timer's configured MilestoneAlerts play, regardless of the list
   * itself. Off by default. */
  alertSoundsEnabled: boolean

  /** Which icon the rotating stat list uses per category. Always fully
   * populated (server fills in defaults), never partial. */
  statIcons: StatIcons

  /** Colors for the top-10 leaderboard panel. Always fully populated
   * (server fills in defaults), never partial. */
  panelColors: PanelColors
}

/** Customizes the top-10 leaderboard panel's colors — see PUT
 * /api/timers/{id}/panel-colors. bg/text are the page's own background
 * and body text; accentBg/accentText are each category's heading bar. */
export interface PanelColors {
  bg: string
  text: string
  accentBg: string
  accentText: string
}

/** One contributor's total in a single "gift category" on the
 * leaderboard panel — see GET /api/timers/{id}/leaderboard. */
export interface LeaderboardEntry {
  username: string
  amount: number
}

/** The top 10 contributors in each of the three "gift categories",
 * most-given first. */
export interface Leaderboard {
  subs: LeaderboardEntry[]
  bits: LeaderboardEntry[]
  donations: LeaderboardEntry[]
}

/** Which of two representations StatIcons' per-category fields hold:
 * a native emoji character (fixed color, set by the emoji font) or a
 * monochrome SVG icon key from a fixed set (colorable via the matching
 * *Color field — see lib/statRotation's SVG_ICON_OPTIONS). */
export type StatIconStyle = 'emoji' | 'svg'

/** Customizes the overlay's rotating stat list's per-category icon —
 * see PUT /api/timers/{id}/stat-icons. subs/bits/donations hold either
 * an emoji character or an SVG icon key depending on style;
 * subs/bits/donationsColor and outline only take effect when style is
 * 'svg' — outline swaps every icon (hand-drawn shape or Unicode glyph
 * alike) from a solid fill to a hollow stroke, shared across all three
 * categories rather than per-category. */
export interface StatIcons {
  style: StatIconStyle
  outline: boolean
  subs: string
  bits: string
  donations: string
  subsColor: string
  bitsColor: string
  donationsColor: string
}

/** Hex colors (e.g. "#111111") customizing the public overlays' pills —
 * see PUT /api/timers/{id}/overlay-colors. timerBg/timerText/moneyBg/
 * moneyText are the main overlay's (/overlay) timer and money-goal
 * pills; goalBg/goalText are the goals overlay's (/goals-overlay) outer
 * pill per milestone, and goalAmountBg/goalAmountText are the smaller
 * dollar-amount pill nested inside each of those. */
export interface OverlayColors {
  timerBg: string
  timerText: string
  moneyBg: string
  moneyText: string
  goalBg: string
  goalText: string
  goalAmountBg: string
  goalAmountText: string
}

/** Lightweight view of a timer used for the picker/list page. */
export interface TimerSummary {
  id: string
  name: string
  running: boolean
  remainingSecs: number
  /** True if the requesting user owns this timer, false if they only
   * moderate it (see Moderator). */
  owner: boolean
}

/** One account granted moderator access to a timer (see
 * GET/POST /api/timers/{id}/moderators) — the same control as the owner,
 * but unable to manage this list itself. */
export interface Moderator {
  userId: string
  displayName: string
}

/** A category of contributor action that adds time to the clock, e.g. a
 * specific sub tier or a per-unit bits/donation rate. */
export type RewardItem =
  | 'tier1_sub'
  | 'tier2_sub'
  | 'tier3_sub'
  | 'gifted_sub'
  | 'gifted_tier1_sub'
  | 'gifted_tier2_sub'
  | 'gifted_tier3_sub'
  | 'bits_100'
  | 'gems_100'
  | 'donation_unit'

/** Seconds awarded per contribution, keyed by item then by platform. */
export type RewardRules = Record<RewardItem, Record<RewardPlatform, number>>

/** Dollars awarded per contribution, keyed by item then by platform — the
 * money equivalent of RewardRules. */
export type MoneyRules = Record<RewardItem, Record<RewardPlatform, number>>

/** One dollar-amount checkpoint along the way to (or past) a timer's
 * MoneyGoal, e.g. "$500: extra hour added". "Reached" isn't stored here —
 * compare amount against the timer's current totalMoneyRaised. */
export interface MoneyMilestone {
  amount: number
  label: string
  /** While true, the dashboard still shows label as-is, but the public
   * goals overlay replaces it with pulsing dots instead — a "surprise"
   * goal whose amount pill (and reached state) is still visible. */
  hidden: boolean
}

/** One of the main overlay's built-in countdown alert chimes — see
 * lib/alertSound.ts's ALERT_SOUND_OPTIONS/playAlertSound, which
 * synthesize each one client-side. */
export type AlertSoundKey =
  | 'chime'
  | 'alarm'
  | 'horn'
  | 'bell'
  | 'siren'
  | 'tornadoSiren'
  | 'trainHorn'

/** One "play a sound when this much time is left" countdown alert for
 * the main overlay — see GET/PUT /api/timers/{id}/milestone-alerts.
 * hours/minutes together give the remaining-time mark it fires at (e.g.
 * hours: 1, minutes: 30 fires at 1h30m left); sound picks which built-in
 * chime plays. A timer can have any number of these, all gated by the
 * single Snapshot.alertSoundsEnabled switch rather than each having its
 * own. */
export interface MilestoneAlert {
  hours: number
  minutes: number
  sound: AlertSoundKey
}

export interface Identity {
  platform: AuthPlatform
  username: string
}

/** Whether a timer has a StreamElements account connected, and its
 * display name if so — never the OAuth2 tokens themselves (see
 * GET/DELETE /api/timers/{id}/stream-elements-token). */
export interface StreamElementsStatus {
  connected: boolean
  displayName?: string
  /** Whether this server has a StreamElements OAuth app configured at
   * all — false means the "Connect" link won't work regardless of this
   * timer's own state (see server.Deps.StreamElementsOAuth). */
  oauthConfigured: boolean
}

/** One Twitch channel the signed-in user could pick as a timer's watched
 * channel: their own linked channel, or one they moderate. See
 * GET /api/twitch/channels. */
export interface TwitchChannelOption {
  id: string
  username: string
  /** True for the signed-in user's own channel, false for one they only
   * moderate. */
  mine: boolean
}

/** One YouTube channel the signed-in user could pick as a timer's
 * watched channel — always their own linked channel, never a "moderated
 * channels" list the way TwitchChannelOption has: YouTube only lets a
 * channel's own linked owner read its live chat. See
 * GET /api/youtube/channels. */
export interface YouTubeChannelOption {
  id: string
  title: string
}

/** GET /api/me: the logged-in user and their linked platform accounts. */
export interface Me {
  id: string
  displayName: string
  identities: Identity[]
}

export type HappyHourAnimationStyle = 'glow' | 'rainbow' | 'sparkles' | 'pulse'

/** Mirrors subathon.HappyHourTrigger: start at a date/time, or once the
 * countdown reaches a mark. */
export type HappyHourTrigger = 'at' | 'remaining'

/** Mirrors subathon.ScheduledHappyHour — a happy hour queued to start
 * later. startAt (ISO timestamp) is set for trigger 'at', remainingSecs
 * for 'remaining'. */
export interface ScheduledHappyHour {
  id: string
  trigger: HappyHourTrigger
  startAt?: string
  remainingSecs?: number
  durationSecs: number
  multiplier: number
  /** Whether its Discord "starting soon" announcement has gone out. */
  announced: boolean
  createdAt: string
}

/** Mirrors subathon.DiscordNotifications' keys: the heads-up before a
 * scheduled happy hour, and the start/end of any happy hour. */
export type NotificationKind = 'headsUp' | 'start' | 'end'

/** Mirrors subathon.DiscordNotification: one kind of announcement's
 * settings. template '' means the server's default text. */
export interface DiscordNotification {
  enabled: boolean
  pingEveryone: boolean
  template: string
}

export type DiscordNotifications = Record<NotificationKind, DiscordNotification>

/** A {placeholder} an announcement's template can use. */
export interface TemplatePlaceholder {
  key: string
  description: string
}

/** A timer's Discord happy hour announcement settings. The webhook URL
 * itself is a secret and never sent back — only whether one is set. */
export interface DiscordSettings {
  configured: boolean
  /** How many minutes before a scheduled happy hour the heads-up is
   * posted. */
  leadMinutes: number
  notifications: DiscordNotifications
  /** How Discord labels the webhook's posts, for the preview — absent if
   * there's no webhook or Discord couldn't be reached. */
  webhookName?: string
  webhookAvatarUrl?: string
  /** Each announcement's built-in text, used while its template is ''. */
  defaultTemplates: Record<NotificationKind, string>
  placeholders: Record<NotificationKind, TemplatePlaceholder[]>
  maxTemplateLength: number
}

/** Mirrors subathon.TwitchAnnouncement: one Twitch chat happy hour
 * announcement's settings. template '' means the default text. */
export interface TwitchAnnouncement {
  enabled: boolean
  template: string
}

/** Mirrors subathon.TwitchAnnouncements — Twitch has no heads-up. */
export interface TwitchAnnouncements {
  start: TwitchAnnouncement
  end: TwitchAnnouncement
}

/** A timer's Twitch chat happy hour announcement settings. */
export interface TwitchAnnouncementSettings {
  /** The Twitch channel they're posted in ('' = none set, nothing is
   * posted). */
  channel: string
  announcements: TwitchAnnouncements
  defaultTemplates: Record<'start' | 'end', string>
  placeholders: Record<'start' | 'end', TemplatePlaceholder[]>
  maxTemplateLength: number
}

/** One announcement for the Discord channel preview: which kind it is,
 * when it's posted, and its exact text (Discord markdown included). */
export interface DiscordPreviewMessage {
  kind: NotificationKind
  label: string
  content: string
}

/** Mirrors subathon.HappyHourStyle — see Snapshot.happyHourStyle. */
export interface HappyHourStyle {
  enabled: boolean
  style: HappyHourAnimationStyle
  /** Hex color, e.g. "#ffc400". Unused by the "rainbow" style. */
  color: string
  /** Shows a "🔥 2× HAPPY HOUR" badge above the timer pill. */
  showBadge: boolean
}
