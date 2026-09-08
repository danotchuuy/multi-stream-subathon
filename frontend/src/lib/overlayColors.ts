import type { OverlayColors, PanelColors } from '../types'

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

/** Mirrors subathon.DefaultPanelColors, used by the leaderboard panel
 * page before its snapshot has loaded. */
export const DEFAULT_PANEL_COLORS: PanelColors = {
  bg: '#0f1115',
  text: '#e6e8eb',
  accentBg: '#7c5cff',
  accentText: '#ffffff',
}
