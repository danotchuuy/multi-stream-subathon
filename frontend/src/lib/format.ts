/** Formats as HH:MM:SS, except the hours segment is dropped entirely
 * (MM:SS) once there's under an hour left — no point padding a clock
 * that's about to read 00:xx:xx with a leading zero hour. */
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds))
  const hours = Math.floor(s / 3600)
  const minutes = Math.floor((s % 3600) / 60)
  const seconds = s % 60

  const pad = (n: number) => n.toString().padStart(2, '0')
  if (hours === 0) return `${pad(minutes)}:${pad(seconds)}`
  return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`
}

/** Formats a dollar amount rounded to the nearest whole dollar, e.g. 500
 * rather than 500.00 — this app's contributions are cheap enough (bits,
 * Kicks, sub tiers) that cents aren't meaningful once totaled. */
export function formatMoney(amount: number): string {
  return Math.round(amount).toString()
}
