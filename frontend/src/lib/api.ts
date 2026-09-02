import type { Me, Platform, Snapshot, TimerSummary } from '../types'

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
    throw new HttpError(
      res.status,
      `${init?.method ?? 'GET'} ${path} failed: ${res.status}`,
    )
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

export function getMe(): Promise<Me> {
  return request('/api/me')
}

/** Provider names ("twitch", "kick") that have credentials configured on
 * the server and so actually work for login/link. */
export function getAuthProviders(): Promise<string[]> {
  return request('/api/auth/providers')
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

/** (Re)initializes the clock to initialSeconds and starts it running,
 * discarding any time left over from a previous run. */
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

export function addManualEvent(
  timerId: string,
  input: {
    platform?: Platform
    username: string
    secondsAdded: number
  },
): Promise<Snapshot> {
  return request(`/api/timers/${timerId}/events`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}
