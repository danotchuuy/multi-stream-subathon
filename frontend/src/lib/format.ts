/** Formats as H:MM:SS, except the hours segment is dropped entirely
 * (M:SS) once there's under an hour left. Only the leading segment goes
 * unpadded (so it never shows a leading zero, e.g. "2:14:37" rather than
 * "02:14:37", or "5:03" rather than "05:03") — every segment after it
 * still pads to 2 digits, same as any normal clock reads. */
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds))
  const hours = Math.floor(s / 3600)
  const minutes = Math.floor((s % 3600) / 60)
  const seconds = s % 60

  const pad = (n: number) => n.toString().padStart(2, '0')
  if (hours === 0) return `${minutes}:${pad(seconds)}`
  return `${hours}:${pad(minutes)}:${pad(seconds)}`
}

/** Formats a dollar amount with 2 decimal places, e.g. 4.75 rather than 5. */
export function formatMoney(amount: number): string {
  return amount.toFixed(2)
}

/** Formats a dollar amount rounded to the nearest whole dollar, no
 * cents — used by the public overlay's money-goal pill, which favors a
 * clean "$120 / $500" read over exact-to-the-cent precision (unlike the
 * dashboard/history, which use formatMoney to track contributions
 * exactly). */
export function formatWholeMoney(amount: number): string {
  return Math.round(amount).toString()
}

/** Abbreviates counts of 1000 or more as e.g. "1.5k"/"12.3k" (always one
 * decimal place); under 1000 renders the plain integer. Used by the
 * overlay's rotating stat list so a long-running subathon's totals don't
 * blow out the pill's width. */
export function formatCompactCount(value: number): string {
  if (value < 1000) return String(value)
  return `${(value / 1000).toFixed(1)}k`
}
