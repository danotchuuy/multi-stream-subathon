import { useEffect, useState } from 'react'
import { formatCompactCount } from './format'
import type { OverlayColors, Snapshot, StatIconStyle, StatIcons } from '../types'

/** How long each category stays on screen before the overlay's rotating
 * stat list advances to the next one. */
export const STAT_ROTATION_INTERVAL_MS = 4000

/** Mirrors subathon.DefaultStatIcons, used before a Snapshot (which
 * always carries the server's actual icons, customized or not) has
 * loaded. */
export const DEFAULT_STAT_ICONS: StatIcons = {
  style: 'emoji',
  outline: false,
  subs: '💜',
  bits: '💎',
  donations: '💵',
  subsColor: '#ffffff',
  bitsColor: '#ffffff',
  donationsColor: '#ffffff',
}

/** subs/bits/donations' default value per icon style — an emoji
 * character isn't a valid SVG icon key or vice versa, so switching style
 * (see Styling.tsx's StatIconsForm) resets to the matching set of
 * defaults here rather than carrying over a value that no longer means
 * anything. Mirrors subathon.defaultStatIconValue server-side. */
export const DEFAULT_ICON_VALUES: Record<
  StatIconStyle,
  { subs: string; bits: string; donations: string }
> = {
  emoji: { subs: '💜', bits: '💎', donations: '💵' },
  svg: { subs: 'heart', bits: 'gem', donations: 'dollar' },
}

/** The emoji choices offered by each category's dropdown on the styling
 * page (see Styling.tsx) — one shared list rather than a curated list
 * per category, so a streamer can pick whatever reads best for their
 * overlay rather than being boxed into a "sub-themed" vs. "money-themed"
 * split. */
export const EMOJI_OPTIONS: { emoji: string; label: string }[] = [
  { emoji: '💜', label: 'Purple heart' },
  { emoji: '❤️', label: 'Red heart' },
  { emoji: '🧡', label: 'Orange heart' },
  { emoji: '💛', label: 'Yellow heart' },
  { emoji: '💚', label: 'Green heart' },
  { emoji: '💙', label: 'Blue heart' },
  { emoji: '🖤', label: 'Black heart' },
  { emoji: '🤍', label: 'White heart' },
  { emoji: '💎', label: 'Gem' },
  { emoji: '⚡', label: 'Lightning' },
  { emoji: '🚀', label: 'Rocket' },
  { emoji: '🔥', label: 'Fire' },
  { emoji: '💰', label: 'Money bag' },
  { emoji: '💵', label: 'Dollar bill' },
  { emoji: '🎁', label: 'Gift box' },
  { emoji: '⭐', label: 'Star' },
  { emoji: '🌟', label: 'Glowing star' },
  { emoji: '👑', label: 'Crown' },
  { emoji: '🎉', label: 'Party popper' },
  { emoji: '💯', label: '100' },
]

/** The monochrome icon choices offered by each category's dropdown when
 * StatIcons.style is 'svg' (see StatSvgIcon) — unlike emoji, these
 * render in whatever color the matching *Color field says. Two kinds:
 * heart/star/gem/gift/crown/bolt/fire/wallet are hand-drawn
 * <path>/<polygon>/<rect> shapes (pixel-identical on every platform);
 * dollar/check/diamond/snowflake/sparkle/note/flag/skull instead render
 * an actual Unicode character via <text> — these are all
 * "text-presentation" code points (Unicode's Emoji_Presentation=No), so
 * unlike 💜/💎/🔥-style emoji, browsers draw them with the UI font
 * rather than the fixed-color emoji font, letting fill="currentColor"
 * reach them like any other text (glyph shape then varies slightly by
 * the viewer's OS, the one trade-off against a hand-drawn path). Keys
 * must match internal/server's statSVGIconKeys exactly. */
export type SvgIconKey =
  | 'heart'
  | 'star'
  | 'gem'
  | 'dollar'
  | 'gift'
  | 'crown'
  | 'bolt'
  | 'fire'
  | 'check'
  | 'diamond'
  | 'snowflake'
  | 'sparkle'
  | 'note'
  | 'flag'
  | 'skull'
  | 'wallet'

export const SVG_ICON_OPTIONS: { key: SvgIconKey; label: string }[] = [
  { key: 'heart', label: 'Heart' },
  { key: 'star', label: 'Star' },
  { key: 'gem', label: 'Gem' },
  { key: 'dollar', label: 'Dollar sign' },
  { key: 'gift', label: 'Gift box' },
  { key: 'crown', label: 'Crown' },
  { key: 'bolt', label: 'Bolt' },
  { key: 'fire', label: 'Fire' },
  { key: 'check', label: 'Check mark' },
  { key: 'diamond', label: 'Diamond' },
  { key: 'snowflake', label: 'Snowflake' },
  { key: 'sparkle', label: 'Sparkle' },
  { key: 'note', label: 'Music note' },
  { key: 'flag', label: 'Flag' },
  { key: 'skull', label: 'Skull' },
  { key: 'wallet', label: 'Wallet' },
]

/** Root <svg> attributes for every StatSvgIcon shape: a fixed 24x24
 * default size (belt-and-braces against the browser's ~300x150 default
 * SVG size in case the surrounding CSS somehow doesn't apply — e.g. a
 * stale cached stylesheet — since an inline <svg> with no explicit
 * size and no resolved CSS size falls back to that instead of its
 * viewBox), plus a solid fill (the default) or a hollow stroke
 * (StatIcons.outline). Applied uniformly, hand-drawn shape or Unicode
 * <text> glyph alike, so toggling outline never depends on which icon
 * happens to be picked, and every icon stays reasonably sized even
 * before any CSS is in play. Callers' own CSS (see .overlay-stats-icon
 * svg / .styling-svg-icon-preview svg) still overrides this default via
 * the normal cascade. */
function svgRootProps(outline: boolean) {
  return {
    viewBox: '0 0 24 24',
    width: 24,
    height: 24,
    ...(outline
      ? {
          fill: 'none',
          stroke: 'currentColor',
          strokeWidth: 1.5,
          strokeLinejoin: 'round' as const,
          strokeLinecap: 'round' as const,
        }
      : { fill: 'currentColor' }),
  }
}

/** Renders one of SVG_ICON_OPTIONS' shapes, colored via currentColor (or
 * stroked, in outline mode) so it always picks up whatever color is set
 * on its container — unlike an emoji, whose color is fixed by the emoji
 * font and can't be overridden. Falls back to the heart shape for an
 * unrecognized key (e.g. stale data from before a key existed) rather
 * than rendering nothing. */
export function StatSvgIcon({
  iconKey,
  outline = false,
}: {
  iconKey: string
  outline?: boolean
}) {
  const rootProps = svgRootProps(outline)

  switch (iconKey) {
    case 'star':
      return (
        <svg {...rootProps} aria-hidden="true">
          <polygon points="12,2 14.9,8.6 22,9.3 16.7,14 18.2,21 12,17.3 5.8,21 7.3,14 2,9.3 9.1,8.6" />
        </svg>
      )
    case 'gem':
      return (
        <svg {...rootProps} aria-hidden="true">
          <polygon points="12,2 2,9 12,22 22,9" />
        </svg>
      )
    case 'dollar':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="16" fontWeight="700">
            $
          </text>
        </svg>
      )
    case 'gift':
      return (
        <svg {...rootProps} aria-hidden="true">
          <rect x="3" y="8" width="18" height="4" rx="1" />
          <rect x="4" y="12" width="16" height="9" rx="1" />
          <rect x="10.5" y="8" width="3" height="13" />
          <path d="M8.5 8c-1.4 0-3-1-3-2.8C5.5 3.7 7 3 8.3 3c1.9 0 3.4 2.2 3.7 5z" />
          <path d="M15.5 8c1.4 0 3-1 3-2.8C18.5 3.7 17 3 15.7 3c-1.9 0-3.4 2.2-3.7 5z" />
        </svg>
      )
    case 'crown':
      return (
        <svg {...rootProps} aria-hidden="true">
          <polygon points="3,18 3,9 8,13 12,5 16,13 21,9 21,18" />
          <rect x="3" y="18" width="18" height="3" rx="1" />
        </svg>
      )
    case 'bolt':
      return (
        <svg {...rootProps} aria-hidden="true">
          <polygon points="13,2 4,14 11,14 9,22 20,10 13,10" />
        </svg>
      )
    case 'fire':
      return (
        <svg {...rootProps} aria-hidden="true">
          <path d="M12 2c1.2 3.2-2.8 4.4-2.8 8.4a2.8 2.8 0 0 0 5.6 0c0-1-.8-2-.8-2.9 2.1 1.3 3.9 3.7 3.9 6.6a5.9 5.9 0 1 1-11.8 0C6.1 8.4 9.9 5.8 12 2z" />
        </svg>
      )
    case 'check':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="18" fontWeight="700">
            ✔
          </text>
        </svg>
      )
    case 'diamond':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="17">
            ◆
          </text>
        </svg>
      )
    case 'snowflake':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="18">
            ❄
          </text>
        </svg>
      )
    case 'sparkle':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="18">
            ✦
          </text>
        </svg>
      )
    case 'note':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="18">
            ♪
          </text>
        </svg>
      )
    case 'flag':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="18">
            ⚑
          </text>
        </svg>
      )
    case 'skull':
      return (
        <svg {...rootProps} aria-hidden="true">
          <text x="12" y="17.5" textAnchor="middle" fontSize="18">
            ☠
          </text>
        </svg>
      )
    case 'wallet':
      return (
        <svg {...rootProps} aria-hidden="true">
          <rect x="2" y="6" width="20" height="13" rx="2.5" />
          <rect x="2" y="6" width="20" height="5" rx="2.5" />
          <circle cx="18" cy="12.5" r="1.6" />
        </svg>
      )
    case 'heart':
    default:
      return (
        <svg {...rootProps} aria-hidden="true">
          <path d="M12 21s-7.5-4.6-10-8.6C.3 9.6 1 6 4 4.6c2.3-1.1 5 0 6.3 2.1L12 8.5l1.7-1.8C15 4.6 17.7 3.5 20 4.6c3 1.4 3.7 5 1.9 7.8C19.5 16.4 12 21 12 21z" />
        </svg>
      )
  }
}

/** The three "gift categories" the rotating stat list cycles through —
 * paired with the Snapshot field each reads its count from and the
 * StatIcons fields each reads its icon/color from. countPrefix prepends
 * a literal string to the displayed count (see RotatingStatPill) — just
 * "$" for tips/donations, since that count is a dollar-flavored total
 * even though it's actually a count of donation events, not a sum of
 * their amounts (see Snapshot.donationsGiven). Order here is rotation
 * order. */
export const STAT_CATEGORIES: {
  key: keyof Pick<Snapshot, 'subsGiven' | 'bitsGiven' | 'donationsGiven'>
  iconKey: 'subs' | 'bits' | 'donations'
  colorKey: 'subsColor' | 'bitsColor' | 'donationsColor'
  label: string
  countPrefix?: string
}[] = [
  { key: 'subsGiven', iconKey: 'subs', colorKey: 'subsColor', label: 'Subs' },
  { key: 'bitsGiven', iconKey: 'bits', colorKey: 'bitsColor', label: 'Bits/Kicks' },
  {
    key: 'donationsGiven',
    iconKey: 'donations',
    colorKey: 'donationsColor',
    label: 'Tips',
    countPrefix: '$',
  },
]

/** Cycles 0..length-1 on a fixed interval — drives which STAT_CATEGORIES
 * entry the overlay/preview currently shows. */
function useRotatingIndex(length: number, intervalMs: number): number {
  const [index, setIndex] = useState(0)

  useEffect(() => {
    if (length <= 1) return
    const id = setInterval(() => {
      setIndex((i) => (i + 1) % length)
    }, intervalMs)
    return () => clearInterval(id)
  }, [length, intervalMs])

  // Keep the index in range if length shrinks (it never does today, but
  // cheap to guard against an out-of-bounds read).
  return index % length
}

/**
 * The overlay's optional rotating stat list: a segment fused into the
 * same merged pill as the money/timer pills (see .overlay-pill-group in
 * Overlay.tsx), cycling through subsGiven/bitsGiven/donationsGiven with
 * an icon per category — either an emoji (fixed color) or a monochrome
 * SVG icon in that category's own color, per icons.style. Renders as a
 * plain .overlay-pill itself — callers are responsible for placing it
 * (and a following .overlay-pill-divider) inside that group, same as the
 * money pill. Shared between Overlay.tsx (the real OBS browser source)
 * and Styling.tsx's live preview so both render identically — true
 * WYSIWYG rather than a preview that just approximates the real thing.
 */
export function RotatingStatPill({
  colors,
  icons,
  subsGiven,
  bitsGiven,
  donationsGiven,
}: {
  colors: Pick<OverlayColors, 'timerBg' | 'timerText'>
  icons: StatIcons
  subsGiven: number
  bitsGiven: number
  donationsGiven: number
}) {
  const index = useRotatingIndex(STAT_CATEGORIES.length, STAT_ROTATION_INTERVAL_MS)
  const category = STAT_CATEGORIES[index]
  const counts = { subsGiven, bitsGiven, donationsGiven }
  const iconValue = icons[category.iconKey]

  return (
    <div
      className="overlay-pill overlay-stats-pill"
      style={{ background: colors.timerBg, color: colors.timerText }}
    >
      <span key={category.key} className="overlay-stats-content">
        <span
          className="overlay-stats-icon"
          aria-hidden="true"
          style={icons.style === 'svg' ? { color: icons[category.colorKey] } : undefined}
        >
          {icons.style === 'svg' ? (
            <StatSvgIcon iconKey={iconValue} outline={icons.outline} />
          ) : (
            iconValue
          )}
        </span>
        <span className="overlay-stats-count">
          {category.countPrefix}
          {formatCompactCount(counts[category.key])}
        </span>
      </span>
    </div>
  )
}
