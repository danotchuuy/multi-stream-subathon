import type {
  AuthPlatform,
  EventType,
  Me,
  Moderator,
  MoneyMilestone,
  MoneyRules,
  OverlayColors,
  Platform,
  RewardRules,
  Snapshot,
  StatIcons,
  StreamElementsStatus,
  SubathonEvent,
  TimerSummary,
  TwitchChannelOption,
  YouTubeChannelOption,
} from '../types'

export class HttpError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    ...init,
  })
  if (!res.ok) {
    // Server handlers respond with a plain-text body via http.Error, e.g.
    // "could not find that Twitch channel" — surface that instead of just
    // the status code so callers can show the actual reason.
    const body = await res.text().catch(() => '')
    throw new HttpError(
      res.status,
      body || `${init?.method ?? 'GET'} ${path} failed: ${res.status}`,
    )
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export function getMe(): Promise<Me> {
  return request('/api/me')
}

/** Unlinks the signed-in user's identity on platform ("twitch" or
 * "kick") — refused (400) if it's their only linked platform, since
 * there's no other way back into the account. */
export function unlinkIdentity(platform: AuthPlatform): Promise<void> {
  return request(`/api/identities/${platform}`, { method: 'DELETE' })
}

/** Provider names ("twitch", "kick") that have credentials configured on
 * the server and so actually work for login/link. */
export function getAuthProviders(): Promise<string[]> {
  return request('/api/auth/providers')
}

/** Twitch channels the signed-in user can pick as a timer's watched
 * channel: their own channel and any they moderate. Empty if they haven't
 * linked Twitch (or Twitch isn't configured on the server). */
export function getTwitchChannels(): Promise<TwitchChannelOption[]> {
  return request('/api/twitch/channels')
}

/** YouTube channel(s) the signed-in user can pick as a timer's watched
 * channel — just their own linked channel(s), never a "moderated
 * channels" list (see YouTubeChannelOption). Empty if they haven't
 * linked YouTube (or YouTube isn't configured on the server). */
export function getYouTubeChannels(): Promise<YouTubeChannelOption[]> {
  return request('/api/youtube/channels')
}

export function logout(): Promise<void> {
  return request('/auth/logout', { method: 'POST' })
}

export function listTimers(): Promise<TimerSummary[]> {
  return request('/api/timers')
}

export function createTimer(name: string): Promise<Snapshot> {
  return request('/api/timers', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
}

export function getState(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/state`)
}

/** This timer's full contribution history (every event ever recorded,
 * not just Snapshot's capped recent list) — backs the /history page.
 * Public, same as getState: the timer ID is the token. */
export function getEventHistory(timerId: string): Promise<SubathonEvent[]> {
  return request(`/api/timers/${timerId}/events`)
}

/** (Re)initializes the clock to initialSeconds, discarding any time left
 * over from a previous run. Leaves the timer stopped — call
 * resumeSubathon separately to start it. */
export function resetSubathon(
  timerId: string,
  initialSeconds: number,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/reset`, {
    method: 'POST',
    body: JSON.stringify({ initialSeconds }),
  })
}

/** Continues the clock from wherever it was frozen by stopSubathon. */
export function resumeSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/resume`, { method: 'POST' })
}

export function stopSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/stop`, { method: 'POST' })
}

/** Locks the timer: new contributions still get recorded (and still
 * count toward the money goal) but stop extending the clock. Same as a
 * channel moderator's "!lock" chat command. */
export function lockSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/lock`, { method: 'POST' })
}

/** Reverses lockSubathon. Same as "!unlock". */
export function unlockSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/unlock`, { method: 'POST' })
}

/** Hides the timer from its public overlay (the dashboard still shows
 * it). Same as a channel moderator's "!hide" chat command. */
export function hideSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/hide`, { method: 'POST' })
}

/** Reverses hideSubathon. Same as "!unhide". */
export function unhideSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/unhide`, { method: 'POST' })
}

/** Ends the timer: unlike lock/hide above, dashboard-only — there's no
 * chat command for it. Stops the timer from receiving any further live
 * Twitch/Kick/StreamElements events and makes it stop responding to
 * every "!timer ..." chat command; doesn't itself pause the clock or
 * block manual events/other dashboard controls. */
export function endSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/end`, { method: 'POST' })
}

/** Reverses endSubathon — resumes live events and chat commands. */
export function unendSubathon(timerId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/control/unend`, { method: 'POST' })
}

/** GET a timer's reward rules (seconds per contribution, by item and
 * platform), with defaults filled in for anything unconfigured. */
export function getRewardRules(timerId: string): Promise<RewardRules> {
  return request(`/api/timers/${timerId}/reward-rules`)
}

/** Replaces a timer's reward rules wholesale. */
export function saveRewardRules(
  timerId: string,
  rules: RewardRules,
): Promise<RewardRules> {
  return request(`/api/timers/${timerId}/reward-rules`, {
    method: 'PUT',
    body: JSON.stringify(rules),
  })
}

/** GET a timer's money rules (dollars per contribution, by item and
 * platform), with $0 defaults filled in for anything unconfigured. */
export function getMoneyRules(timerId: string): Promise<MoneyRules> {
  return request(`/api/timers/${timerId}/money-rules`)
}

/** Replaces a timer's money rules wholesale. */
export function saveMoneyRules(
  timerId: string,
  rules: MoneyRules,
): Promise<MoneyRules> {
  return request(`/api/timers/${timerId}/money-rules`, {
    method: 'PUT',
    body: JSON.stringify(rules),
  })
}

/** Sets (or, with goal 0, clears) a timer's dollar goal. */
export function setMoneyGoal(
  timerId: string,
  goal: number,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/money-goal`, {
    method: 'PUT',
    body: JSON.stringify({ goal }),
  })
}

/** Directly overrides a timer's running dollar total, e.g. to reconcile
 * against an external donation tracker rather than relying solely on
 * recorded events. Unlike addManualEvent, this doesn't add a history
 * entry. */
export function setMoneyRaised(
  timerId: string,
  amount: number,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/money-raised`, {
    method: 'PUT',
    body: JSON.stringify({ amount }),
  })
}

/** GET a timer's configured dollar-amount milestones, ordered by amount
 * ascending. Public, same as getState: the timer ID is the token. */
export function getMoneyMilestones(
  timerId: string,
): Promise<MoneyMilestone[]> {
  return request(`/api/timers/${timerId}/money-milestones`)
}

/** Replaces a timer's milestone list wholesale. */
export function saveMoneyMilestones(
  timerId: string,
  milestones: MoneyMilestone[],
): Promise<MoneyMilestone[]> {
  return request(`/api/timers/${timerId}/money-milestones`, {
    method: 'PUT',
    body: JSON.stringify(milestones),
  })
}

/** Changes the public overlay's timer/money-goal pill colors (hex, e.g.
 * "#111111"). An empty field falls back to the server's defaults. */
export function setOverlayColors(
  timerId: string,
  colors: OverlayColors,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/overlay-colors`, {
    method: 'PUT',
    body: JSON.stringify(colors),
  })
}

/** Turns the main overlay's rotating stat list (subs/bits-Kicks/
 * donations, far left of the timer/money pills) on or off. */
export function setStatsRotation(
  timerId: string,
  enabled: boolean,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/stats-rotation`, {
    method: 'PUT',
    body: JSON.stringify({ enabled }),
  })
}

/** Directly overrides the rotating stat list's three running totals
 * (subs/bits-Kicks/donations given), e.g. to correct a miscount or
 * backfill a value from before this feature existed. Unlike addManualEvent,
 * this doesn't add a history entry. */
export function setContributionCounts(
  timerId: string,
  counts: { subsGiven: number; bitsGiven: number; donationsGiven: number },
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/contribution-counts`, {
    method: 'PUT',
    body: JSON.stringify(counts),
  })
}

/** Changes which emoji the rotating stat list uses per category. An
 * empty field falls back to the server's defaults. */
export function setStatIcons(
  timerId: string,
  icons: StatIcons,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/stat-icons`, {
    method: 'PUT',
    body: JSON.stringify(icons),
  })
}

/** Sets (or, with an empty username, clears) the Twitch channel this
 * timer watches for live subs/gift-subs/cheers. */
export function setTwitchChannel(
  timerId: string,
  username: string,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/twitch-channel`, {
    method: 'PUT',
    body: JSON.stringify({ username }),
  })
}

/** Sets (or, with an empty username, clears) the Kick channel this timer
 * watches for live subs/gift-subs. */
export function setKickChannel(
  timerId: string,
  username: string,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/kick-channel`, {
    method: 'PUT',
    body: JSON.stringify({ username }),
  })
}

/** Sets (or, with an empty channelId, clears) the YouTube channel this
 * timer watches for live Super Chats/Super Stickers/new members/gifted
 * memberships. Unlike setTwitchChannel/setKickChannel, channelId must be
 * one the signed-in user has themselves linked via "Sign in with
 * Google"/"Link YouTube" (see getYouTubeChannels) — not free text. */
export function setYouTubeChannel(
  timerId: string,
  channelId: string,
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/youtube-channel`, {
    method: 'PUT',
    body: JSON.stringify({ channelId }),
  })
}

/** Whether this timer has a StreamElements account connected. */
export function getStreamElementsStatus(
  timerId: string,
): Promise<StreamElementsStatus> {
  return request(`/api/timers/${timerId}/stream-elements-token`)
}

/** Disconnects this timer's StreamElements account. Connecting one
 * happens via OAuth2 instead (see streamElementsOAuthStartUrl) — there's
 * no token for the frontend to submit directly. */
export function disconnectStreamElements(
  timerId: string,
): Promise<StreamElementsStatus> {
  return request(`/api/timers/${timerId}/stream-elements-token`, {
    method: 'DELETE',
  })
}

/** The URL to send the browser to (a plain navigation, not a fetch — it
 * ends in a redirect to StreamElements' own authorize page) to connect
 * this timer's StreamElements account. Requires being logged in as this
 * timer's owner/moderator, same as every other control endpoint — unlike
 * those, this one is reached via a top-level navigation (see
 * StreamElementsForm's "Connect" link) so the session cookie rides along
 * automatically rather than needing a separate Authorization header. */
export function streamElementsOAuthStartUrl(timerId: string): string {
  return `/api/timers/${timerId}/stream-elements/oauth/start`
}

/** This timer's current moderators (not including its owner). Viewable by
 * the owner and existing moderators alike. */
export function getModerators(timerId: string): Promise<Moderator[]> {
  return request(`/api/timers/${timerId}/moderators`)
}

/** Grants moderator access to this timer to whichever account has
 * username linked on Twitch or Kick. Owner only. */
export function addModerator(
  timerId: string,
  username: string,
): Promise<Moderator> {
  return request(`/api/timers/${timerId}/moderators`, {
    method: 'POST',
    body: JSON.stringify({ username }),
  })
}

/** Revokes a moderator's access to this timer. Owner only. */
export function removeModerator(
  timerId: string,
  userId: string,
): Promise<void> {
  return request(`/api/timers/${timerId}/moderators/${userId}`, {
    method: 'DELETE',
  })
}

/** Deletes a previously recorded event and reverses its effect on the
 * clock/money goal — the dashboard's recent-contributors "remove" control.
 * Owner/moderator only; 404s if eventID isn't among the timer's recent
 * events. */
export function removeEvent(timerId: string, eventId: string): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/events/${eventId}`, {
    method: 'DELETE',
  })
}

export function addManualEvent(
  timerId: string,
  input: {
    platform?: Platform
    type?: EventType
    username: string
    secondsAdded: number
    moneyAdded?: number
    amount?: number
  },
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/events`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}
