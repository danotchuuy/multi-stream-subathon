import type { OverlayColors } from '../types'

/** Mirrors subathon.DefaultOverlayColors, used by the overlay pages
 * before their snapshot (which always carries the server's actual
 * colors, customized or not) has loaded. */
export const DEFAULT_OVERLAY_COLORS: OverlayColors = {
  timerBg: '#111111',
  timerText: '#ffffff',
  moneyBg: '#111111',
  moneyText: '#ffffff',
  goalBg: '#111111',
  goalText: '#ffffff',
  goalAmountBg: '#ffffff',
  goalAmountText: '#111111',
}
