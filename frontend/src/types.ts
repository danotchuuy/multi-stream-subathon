// Mirrors the JSON shapes produced by internal/subathon (Timer, Manager)
// and internal/server (auth handlers) on the Go backend.

/** Platforms an account identity can be linked to (login or link). */
export type AuthPlatform = 'twitch' | 'kick' | 'youtube'

export type Platform = 'kick' | 'youtube' | 'twitch' | 'manual'

export type EventType = 'sub' | 'gifted_sub' | 'donation' | 'bits' | 'manual'

export interface SubathonEvent {
  id: string
  timerId: string
  platform: Platform
  type: EventType
  username: string
  secondsAdded: number
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
}

/** Lightweight view of a timer used for the picker/list page. */
export interface TimerSummary {
  id: string
  name: string
  running: boolean
  remainingSecs: number
}

export interface Identity {
  platform: AuthPlatform
  username: string
}

/** GET /api/me: the logged-in user and their linked platform accounts. */
export interface Me {
  id: string
  displayName: string
  identities: Identity[]
}
