import { useMemo } from 'react'

/** Mirrors subathon.DefaultExplosionColor, used before a Snapshot (which
 * always carries the server's actual color, customized or not) has
 * loaded. */
export const DEFAULT_EXPLOSION_COLOR = '#ff8c00'

/** How many debris shards fly out of the timer pill on the finale burst
 * — see ExplosionBurst. */
const SHARD_COUNT = 14

/** How many lingering ember specks drift up afterwards, on an infinite
 * loop, so the exploded timer keeps looking "alight" rather than going
 * still once the one-shot burst animation finishes. */
const EMBER_COUNT = 22

/** How far each shard's shade is pushed from the base color (toward
 * white if positive, toward black if negative) — cycled through so a
 * single picked color still fans out into a few tones instead of every
 * shard being identical. */
const SHARD_SHADES = [0.3, 0, -0.2, -0.4]

interface Shard {
  angle: number
  distance: number
  size: number
  delay: number
  color: string
}

interface Ember {
  left: number
  size: number
  delay: number
}

function hexToRgb(hex: string): [number, number, number] {
  let normalized = hex.replace('#', '')
  if (normalized.length === 3) {
    normalized = normalized
      .split('')
      .map((c) => c + c)
      .join('')
  }
  const num = parseInt(normalized, 16)
  if (normalized.length !== 6 || Number.isNaN(num)) {
    return hexToRgb(DEFAULT_EXPLOSION_COLOR)
  }
  return [(num >> 16) & 255, (num >> 8) & 255, num & 255]
}

function rgbToHex(r: number, g: number, b: number): string {
  const clamp = (v: number) => Math.max(0, Math.min(255, Math.round(v)))
  return `#${[r, g, b].map((v) => clamp(v).toString(16).padStart(2, '0')).join('')}`
}

/** Blends hex toward white (amount > 0) or black (amount < 0) by amount
 * (0–1) — used to fan a single user-picked explosion color out into a
 * few shades for shard variety, same idea as a CSS color-mix() but
 * computed once in JS so each shard can get its own fixed shade rather
 * than redeclaring a CSS class per tone. */
function shade(hex: string, amount: number): string {
  const [r, g, b] = hexToRgb(hex)
  const target = amount > 0 ? 255 : 0
  const t = Math.abs(amount)
  return rgbToHex(r + (target - r) * t, g + (target - g) * t, b + (target - b) * t)
}

/** Builds SHARD_COUNT shards fanned out in a full circle (with a little
 * jitter so they don't look mechanically even) plus EMBER_COUNT embers
 * scattered along the bottom, every shard shaded off the given base
 * color (see SHARD_SHADES). Pure function so it's easy to call once from
 * useMemo rather than reshuffling on every render. */
function buildDebris(color: string): { shards: Shard[]; embers: Ember[] } {
  const shardColors = SHARD_SHADES.map((s) => shade(color, s))

  const shards: Shard[] = []
  for (let i = 0; i < SHARD_COUNT; i++) {
    const baseAngle = (360 / SHARD_COUNT) * i
    shards.push({
      angle: baseAngle + (Math.random() * 20 - 10),
      distance: 60 + Math.random() * 70,
      size: 8 + Math.random() * 10,
      delay: Math.random() * 0.15,
      color: shardColors[i % shardColors.length],
    })
  }

  const embers: Ember[] = []
  for (let i = 0; i < EMBER_COUNT; i++) {
    embers.push({
      left: 10 + Math.random() * 80,
      size: 3 + Math.random() * 4,
      delay: Math.random() * 2.4,
    })
  }

  return { shards, embers }
}

/** The inline style that applies color to an explosion: sets the
 * --explosion-color CSS variable the flash/ring/embers/glow (see
 * index.css's .overlay-explosion-* rules) read their tint from. Apply
 * this to whatever element wraps both <ExplosionBurst> and the
 * .overlay-pill-group it's attached to (its glow keyframe needs the
 * variable too), e.g. the .overlay-explosion-wrap div. */
export function explosionColorStyle(color: string): React.CSSProperties {
  return { '--explosion-color': color } as React.CSSProperties
}

/**
 * The main overlay's "timer explodes" finale (see Snapshot.
 * explosionEnabled/explosionColor) — a flash, a shockwave ring and a
 * burst of flying shards that each play once, followed by an infinite
 * loop of drifting embers so the pill keeps looking "destroyed" rather
 * than just resetting to a plain still frame. Pure CSS animation (see
 * index.css's .overlay-explosion-* rules); this component's only job is
 * to generate a stable, varied set of shard/ember positions once (via
 * useMemo) and render the divs those CSS rules key off.
 *
 * Mount this once — the moment it mounts is the moment the burst plays,
 * since every one-shot animation here is tied to element creation, not
 * to a prop change. Overlay.tsx (and Styling.tsx's preview) control
 * *when* that is by only rendering it once remainingSecs has reached 0.
 * color only affects the shards' computed shades (SHARD_SHADES) — the
 * flash/ring/embers pick up the same color via the --explosion-color
 * variable set by explosionColorStyle instead, so changing it live (e.g.
 * while previewing) doesn't need this component to remount.
 */
export function ExplosionBurst({ color = DEFAULT_EXPLOSION_COLOR }: { color?: string }) {
  const { shards, embers } = useMemo(() => buildDebris(color), [color])

  return (
    <div className="overlay-explosion-burst" aria-hidden="true">
      <div className="overlay-explosion-flash" />
      <div className="overlay-explosion-ring" />
      {shards.map((s, i) => (
        <div
          key={i}
          className="overlay-explosion-shard"
          style={
            {
              '--angle': `${s.angle}deg`,
              '--distance': `${s.distance}px`,
              '--size': `${s.size}px`,
              '--delay': `${s.delay}s`,
              '--shard-color': s.color,
            } as React.CSSProperties
          }
        />
      ))}
      {embers.map((e, i) => (
        <div
          key={i}
          className="overlay-explosion-ember"
          style={
            {
              '--left': `${e.left}%`,
              '--size': `${e.size}px`,
              '--delay': `${e.delay}s`,
            } as React.CSSProperties
          }
        />
      ))}
    </div>
  )
}
