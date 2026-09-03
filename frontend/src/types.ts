// Mirrors the JSON shapes produced by internal/subathon (Timer, Manager)
// and internal/server (auth handlers) on the Go backend.

/** Platforms an account identity can be linked to (login or link). */
export type AuthPlatform = 'twitch' | 'kick' | 'youtube'

export type Platform = 'kick' | 'youtube' | 'twitch' | 'streamelements' | 'manual'

/** Platforms reward/money rates are configured for — every Platform
 * except 'manual', which specifies its own seconds/dollars directly
 * rather than looking one up (see RewardRules/MoneyRules). */
export type RewardPlatform = 'twitch' | 'kick' | 'youtube' | 'streamelements'

export type EventType = 'sub' | 'gifted_sub' | 'donation' | 'bits' | 'manual'

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
  /** Colors for the public overlays' pills. Always fully populated
   * (server fills in defaults), never partial. */
  overlayColors: OverlayColors
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
  | 'bits_100'
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

export interface Identity {
  platform: AuthPlatform
  username: string
}

/** Whether a timer has a StreamElements account connected, and its
 * display name if so — never the JWT token itself (see
 * GET/PUT /api/timers/{id}/stream-elements-token). */
export interface StreamElementsStatus {
  connected: boolean
  displayName?: string
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

/** GET /api/me: the logged-in user and their linked platform accounts. */
export interface Me {
  id: string
  displayName: string
  identities: Identity[]
}
