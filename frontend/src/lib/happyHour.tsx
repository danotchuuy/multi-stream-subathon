import { useMemo } from 'react'
import type { HappyHourAnimationStyle, HappyHourStyle } from '../types'

/** Mirrors subathon.DefaultHappyHourStyle, used before a Snapshot (which
 * always carries the server's actual style, customized or not) has
 * loaded. */
export const DEFAULT_HAPPY_HOUR_STYLE: HappyHourStyle = {
  enabled: true,
  style: 'glow',
  color: '#ffc400',
  showBadge: true,
}

/** Every animation style, in the order the Styling page lists them. */
export const HAPPY_HOUR_ANIMATION_STYLES: {
  value: HappyHourAnimationStyle
  label: string
  description: string
}[] = [
  { value: 'glow', label: 'Glow', description: 'A pulsing colored halo around the timer' },
  { value: 'rainbow', label: 'Rainbow', description: 'A cycling rainbow ring (ignores the color)' },
  { value: 'sparkles', label: 'Sparkles', description: 'Sparkles twinkling around the timer' },
  { value: 'pulse', label: 'Pulse', description: 'The timer gently breathes in and out' },
]

/** How many sparkles the "sparkles" style scatters around the pill. */
const SPARKLE_COUNT = 24

interface Sparkle {
  left: number
  /** CSS top, e.g. "-14px" (above the pill) or "calc(100% + 4px)"
   * (below it). */
  top: string
  size: number
  delay: number
  duration: number
}

/** Scatters SPARKLE_COUNT sparkles around the pill's edges (mostly just
 * outside it, so they don't cover the clock digits). */
function buildSparkles(): Sparkle[] {
  const sparkles: Sparkle[] = []
  for (let i = 0; i < SPARKLE_COUNT; i++) {
    const onTop = i % 2 === 0
    sparkles.push({
      left: -4 + Math.random() * 108,
      top: onTop
        ? `${-20 + Math.random() * 14}px`
        : `calc(100% + ${Math.random() * 10 - 4}px)`,
      size: 10 + Math.random() * 10,
      delay: Math.random() * 1.8,
      duration: 1.2 + Math.random() * 1,
    })
  }
  return sparkles
}

/** Black or white, whichever reads better on hex — for the badge text. */
function contrastText(hex: string): string {
  const num = parseInt(hex.replace('#', ''), 16)
  if (Number.isNaN(num)) return '#000000'
  const r = (num >> 16) & 255
  const g = (num >> 8) & 255
  const b = num & 255
  return 0.299 * r + 0.587 * g + 0.114 * b > 150 ? '#000000' : '#ffffff'
}

/** Formats a boost multiplier for the badge, e.g. 2 → "2×", 1.5 → "1.5×". */
function formatMultiplier(multiplier: number): string {
  return `${Number(multiplier.toFixed(2))}×`
}

/**
 * Wraps the main overlay's pill group with its happy hour animation (see
 * Snapshot.happyHourStyle) while active — or renders children untouched
 * while not. Pure CSS animation (see index.css's .overlay-hh-* rules);
 * shared by Overlay.tsx and Styling.tsx's preview so both render the
 * exact same markup.
 */
export function HappyHourWrap({
  active,
  happyHourStyle,
  multiplier,
  children,
}: {
  active: boolean
  happyHourStyle: HappyHourStyle
  multiplier: number
  children: React.ReactNode
}) {
  const sparkles = useMemo(() => buildSparkles(), [])

  if (!active || !happyHourStyle.enabled) {
    return <>{children}</>
  }

  const { style, color, showBadge } = happyHourStyle
  return (
    <div
      className={`overlay-hh-wrap overlay-hh-${style}`}
      style={
        {
          '--hh-color': color,
          '--hh-text': contrastText(color),
        } as React.CSSProperties
      }
    >
      {showBadge && (
        <div className="overlay-hh-badge">
          🔥 {formatMultiplier(multiplier)} HAPPY HOUR
        </div>
      )}
      {style === 'sparkles' && (
        <div className="overlay-hh-sparkle-field" aria-hidden="true">
          {sparkles.map((s, i) => (
            <span
              key={i}
              className="overlay-hh-sparkle"
              style={
                {
                  left: `${s.left}%`,
                  top: s.top,
                  '--size': `${s.size}px`,
                  '--delay': `${s.delay}s`,
                  '--duration': `${s.duration}s`,
                } as React.CSSProperties
              }
            />
          ))}
        </div>
      )}
      {children}
    </div>
  )
}
