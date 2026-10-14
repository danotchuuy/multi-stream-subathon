import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import {
  getLeaderboard,
  getMilestoneAlerts,
  getMoneyMilestones,
  saveMilestoneAlerts,
  setAlertSounds,
  setContributionCounts,
  setExplosion,
  setExplosionColor,
  setOverlayColors,
  setPanelColors,
  setStatIcons,
  setStatsRotation,
  HttpError,
} from '../lib/api'
import { formatCompactCount, formatDuration, formatWholeMoney } from '../lib/format'
import { ALERT_SOUND_OPTIONS, DEFAULT_ALERT_SOUND, playAlertSound } from '../lib/alertSound'
import { explosionColorStyle, ExplosionBurst } from '../lib/explosion'
import {
  CategoryIcon,
  DEFAULT_ICON_VALUES,
  EMOJI_OPTIONS,
  RotatingStatPill,
  STAT_CATEGORIES,
  StatSvgIcon,
  SVG_ICON_OPTIONS,
} from '../lib/statRotation'
import type {
  Leaderboard,
  MilestoneAlert,
  MoneyMilestone,
  OverlayColors,
  PanelColors,
  StatIconStyle,
  StatIcons,
} from '../types'

function controlErrorMessage(err: unknown): string {
  if (err instanceof HttpError) {
    if (err.status === 403) return "You don't own this timer."
    if (err.status === 401) return 'Please log in again.'
    if (err.message) return err.message
  }
  return 'That action failed. Please try again.'
}

/** A handful of ready-made overlayColors combinations — a starting point
 * to tweak from rather than picking all 8 hex values by hand. Purely a
 * client-side convenience; nothing is saved until the Save button below
 * is pressed. */
const PRESETS: { name: string; colors: OverlayColors }[] = [
  {
    name: 'Classic dark',
    colors: {
      timerBg: '#111111',
      timerText: '#ffffff',
      moneyBg: '#111111',
      moneyText: '#ffffff',
      goalBg: '#111111',
      goalText: '#ffffff',
      goalAmountBg: '#ffffff',
      goalAmountText: '#111111',
    },
  },
  {
    name: 'Clean light',
    colors: {
      timerBg: '#ffffff',
      timerText: '#111111',
      moneyBg: '#ffffff',
      moneyText: '#111111',
      goalBg: '#ffffff',
      goalText: '#111111',
      goalAmountBg: '#111111',
      goalAmountText: '#ffffff',
    },
  },
  {
    name: 'Twitch purple',
    colors: {
      timerBg: '#9146ff',
      timerText: '#ffffff',
      moneyBg: '#772ce8',
      moneyText: '#ffffff',
      goalBg: '#9146ff',
      goalText: '#ffffff',
      goalAmountBg: '#ffffff',
      goalAmountText: '#772ce8',
    },
  },
  {
    name: 'Kick green',
    colors: {
      timerBg: '#53fc18',
      timerText: '#0f1115',
      moneyBg: '#0f1115',
      moneyText: '#53fc18',
      goalBg: '#53fc18',
      goalText: '#0f1115',
      goalAmountBg: '#0f1115',
      goalAmountText: '#53fc18',
    },
  },
  {
    name: 'Sunset',
    colors: {
      timerBg: '#ff5e62',
      timerText: '#ffffff',
      moneyBg: '#ff9966',
      moneyText: '#3a1400',
      goalBg: '#ff5e62',
      goalText: '#ffffff',
      goalAmountBg: '#ffe29a',
      goalAmountText: '#7a3b00',
    },
  },
  {
    name: 'Neon',
    colors: {
      timerBg: '#0d0221',
      timerText: '#0ff0fc',
      moneyBg: '#0d0221',
      moneyText: '#ff00e6',
      goalBg: '#0d0221',
      goalText: '#0ff0fc',
      goalAmountBg: '#ff00e6',
      goalAmountText: '#0d0221',
    },
  },
]

/** A miniature version of the same pill markup Overlay.tsx renders, sized
 * down to sit inside a preset swatch button — gives each preset a
 * recognizable thumbnail instead of just a name. */
function PresetSwatch({ colors }: { colors: OverlayColors }) {
  return (
    <span className="preset-swatch">
      <span
        className="preset-swatch-pill"
        style={{ background: colors.moneyBg, color: colors.moneyText }}
      >
        $
      </span>
      <span
        className="preset-swatch-pill"
        style={{ background: colors.timerBg, color: colors.timerText }}
      >
        00:00
      </span>
    </span>
  )
}

/** Counts the rotating stat list's three fields as edited by
 * ContributionCountsForm below — same shape the setContributionCounts API
 * call takes. */
type ContributionCounts = {
  subsGiven: number
  bitsGiven: number
  donationsGiven: number
}

/** Lets the owner/moderator directly override the rotating stat list's
 * three running totals (see STAT_CATEGORIES) — e.g. to correct a
 * miscount, or seed values for history from before this feature existed.
 * Its inputs' starting values are captured once, on mount, same as
 * Dashboard's ChannelForm/MoneyRaisedForm — only mount this once the
 * current values are known (i.e. after the snapshot has loaded). */
function ContributionCountsForm({
  initialCounts,
  onSave,
}: {
  initialCounts: ContributionCounts
  onSave: (counts: ContributionCounts) => Promise<unknown>
}) {
  const [counts, setCounts] = useState(initialCounts)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleChange = (field: keyof ContributionCounts, value: string) => {
    setCounts((prev) => ({ ...prev, [field]: Math.max(0, Number(value) || 0) }))
    setSaved(false)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave(counts)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group control-group-stacked" onSubmit={handleSubmit}>
      <div className="styling-stats-counts-grid">
        {STAT_CATEGORIES.map((cat) => (
          <label key={cat.key}>
            {cat.label} given
            <input
              type="number"
              min={0}
              step={1}
              value={counts[cat.key]}
              onChange={(e) => handleChange(cat.key, e.target.value)}
            />
          </label>
        ))}
      </div>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save counts'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** Lets the owner/moderator pick each rotating stat list category's icon
 * — either an emoji (EMOJI_OPTIONS, fixed color) or a monochrome SVG
 * icon (SVG_ICON_OPTIONS) plus its own color picker, toggled by the
 * "Icon style" radio pair. Switching style resets subs/bits/donations to
 * that style's own defaults (DEFAULT_ICON_VALUES) rather than carrying
 * over a value that means nothing in the other representation (an emoji
 * character isn't a valid SVG icon key or vice versa). The "Outline"
 * checkbox (svg style only) swaps every category's icon from solid to
 * hollow-stroked at once — see StatSvgIcon. icons/onChange are
 * controlled by the parent (same lifted-state pattern as colors below)
 * so the preview canvas above reflects every edit immediately, not just
 * what was last saved. */
function StatIconsForm({
  icons,
  onChange,
  onSave,
}: {
  icons: StatIcons
  onChange: (icons: StatIcons) => void
  onSave: () => Promise<unknown>
}) {
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleStyleChange = (style: StatIconStyle) => {
    onChange({ ...icons, style, ...DEFAULT_ICON_VALUES[style] })
    setSaved(false)
  }

  const handleIconChange = (field: 'subs' | 'bits' | 'donations', value: string) => {
    onChange({ ...icons, [field]: value })
    setSaved(false)
  }

  const handleColorChange = (
    field: 'subsColor' | 'bitsColor' | 'donationsColor',
    value: string,
  ) => {
    onChange({ ...icons, [field]: value })
    setSaved(false)
  }

  const handleOutlineChange = (outline: boolean) => {
    onChange({ ...icons, outline })
    setSaved(false)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave()
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group control-group-stacked" onSubmit={handleSubmit}>
      <fieldset className="styling-icon-style-fieldset">
        <legend>Icon style</legend>
        <label className="checkbox-label">
          <input
            type="radio"
            name="stat-icon-style"
            checked={icons.style === 'emoji'}
            onChange={() => handleStyleChange('emoji')}
          />
          Emoji
        </label>
        <label className="checkbox-label">
          <input
            type="radio"
            name="stat-icon-style"
            checked={icons.style === 'svg'}
            onChange={() => handleStyleChange('svg')}
          />
          Monochrome icon (custom color)
        </label>
      </fieldset>

      {icons.style === 'svg' && (
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={icons.outline}
            onChange={(e) => handleOutlineChange(e.target.checked)}
          />
          Outline instead of solid
        </label>
      )}

      <div className="styling-stats-icons-grid">
        {STAT_CATEGORIES.map((cat) => (
          <label key={cat.iconKey}>
            {cat.label} icon
            {icons.style === 'svg' ? (
              <span className="styling-svg-icon-row">
                <select
                  value={icons[cat.iconKey]}
                  onChange={(e) => handleIconChange(cat.iconKey, e.target.value)}
                >
                  {SVG_ICON_OPTIONS.map((opt) => (
                    <option key={opt.key} value={opt.key}>
                      {opt.label}
                    </option>
                  ))}
                </select>
                <input
                  type="color"
                  value={icons[cat.colorKey]}
                  onChange={(e) => handleColorChange(cat.colorKey, e.target.value)}
                  aria-label={`${cat.label} icon color`}
                />
                <span
                  className="styling-svg-icon-preview"
                  style={{ color: icons[cat.colorKey] }}
                >
                  <StatSvgIcon iconKey={icons[cat.iconKey]} outline={icons.outline} />
                </span>
              </span>
            ) : (
              <select
                value={icons[cat.iconKey]}
                onChange={(e) => handleIconChange(cat.iconKey, e.target.value)}
              >
                {EMOJI_OPTIONS.map((opt) => (
                  <option key={opt.emoji} value={opt.emoji}>
                    {opt.emoji} {opt.label}
                  </option>
                ))}
              </select>
            )}
          </label>
        ))}
      </div>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save icons'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** Lets the owner/moderator pick the leaderboard panel's colors — bg/
 * text for the page itself, accentBg/accentText for each category's
 * heading bar (see PanelColors). colors/onChange are controlled by the
 * parent, same lifted-state pattern as the overlay colors/icons above,
 * so the preview below reflects every edit immediately. */
function PanelColorsForm({
  colors,
  onChange,
  onSave,
}: {
  colors: PanelColors
  onChange: (colors: PanelColors) => void
  onSave: () => Promise<unknown>
}) {
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleChange = (field: keyof PanelColors, value: string) => {
    onChange({ ...colors, [field]: value })
    setSaved(false)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave()
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group control-group-stacked" onSubmit={handleSubmit}>
      <div className="overlay-colors-grid">
        <label>
          Page background
          <input
            type="color"
            value={colors.bg}
            onChange={(e) => handleChange('bg', e.target.value)}
          />
        </label>
        <label>
          Page text
          <input
            type="color"
            value={colors.text}
            onChange={(e) => handleChange('text', e.target.value)}
          />
        </label>
        <label>
          Category heading background
          <input
            type="color"
            value={colors.accentBg}
            onChange={(e) => handleChange('accentBg', e.target.value)}
          />
        </label>
        <label>
          Category heading text
          <input
            type="color"
            value={colors.accentText}
            onChange={(e) => handleChange('accentText', e.target.value)}
          />
        </label>
      </div>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save panel colors'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** Lets the owner/moderator pick the "timer explodes" finale's color —
 * tints the flash/ring/shards/embers (see ExplosionBurst). color/
 * onChange are controlled by the parent, same lifted-state pattern as
 * the overlay colors/icons/panel colors above, so the "Preview
 * explosion" button reflects an edit immediately, even before it's
 * saved. */
function ExplosionColorForm({
  color,
  onChange,
  onSave,
}: {
  color: string
  onChange: (color: string) => void
  onSave: () => Promise<unknown>
}) {
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setSaved(false)
    setError(null)
    try {
      await onSave()
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="control-group control-group-stacked" onSubmit={handleSubmit}>
      <label>
        Explosion color
        <input
          type="color"
          value={color}
          onChange={(e) => {
            onChange(e.target.value)
            setSaved(false)
          }}
        />
      </label>
      <button type="submit" disabled={busy}>
        {saved ? 'Saved!' : 'Save explosion color'}
      </button>
      {error && <p className="error-message">{error}</p>}
    </form>
  )
}

/** A fresh timer's starting point for the countdown alert list below —
 * purely a client-side convenience (same idea as PRESETS for overlay
 * colors): shown once the fetched list comes back empty, so a streamer
 * turning the master toggle on for the first time sees a sensible
 * starting setup rather than a blank list, but nothing is saved until
 * Save is pressed. */
const DEFAULT_ALERTS: MilestoneAlert[] = [
  { hours: 10, minutes: 0, sound: DEFAULT_ALERT_SOUND },
  { hours: 5, minutes: 0, sound: DEFAULT_ALERT_SOUND },
  { hours: 1, minutes: 0, sound: DEFAULT_ALERT_SOUND },
]

/** Lets the owner/moderator build the "when should it chime, and with
 * what sound" countdown alert list (see Timer.MilestoneAlerts) — same
 * fetch-its-own-data, add/remove-rows, explicit-Save shape as Rewards.tsx's
 * MilestonesEditor. Each row's ▶ button plays that row's sound
 * immediately, so picking one doesn't require saving and waiting for
 * the clock to reach it first. */
function MilestoneAlertsEditor({ timerId }: { timerId: string }) {
  const [alerts, setAlerts] = useState<MilestoneAlert[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getMilestoneAlerts(timerId)
      .then((loaded) => setAlerts(loaded.length > 0 ? loaded : DEFAULT_ALERTS))
      .catch(() => setError('Failed to load countdown alerts.'))
  }, [timerId])

  const handleChange = (
    index: number,
    field: 'hours' | 'minutes',
    value: string,
  ) => {
    setAlerts((prev) => {
      if (!prev) return prev
      const next = [...prev]
      const parsed = Math.max(0, Math.trunc(Number(value)) || 0)
      next[index] = {
        ...next[index],
        [field]: field === 'minutes' ? Math.min(59, parsed) : parsed,
      }
      return next
    })
    setSaved(false)
  }

  const handleSoundChange = (index: number, sound: MilestoneAlert['sound']) => {
    setAlerts((prev) => {
      if (!prev) return prev
      const next = [...prev]
      next[index] = { ...next[index], sound }
      return next
    })
    setSaved(false)
  }

  const handleAdd = () => {
    setAlerts((prev) => [
      ...(prev ?? []),
      { hours: 0, minutes: 30, sound: DEFAULT_ALERT_SOUND },
    ])
    setSaved(false)
  }

  const handleRemove = (index: number) => {
    setAlerts((prev) => prev?.filter((_, i) => i !== index) ?? prev)
    setSaved(false)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!alerts) return
    setError(null)
    const cleaned = [...alerts].sort(
      (a, b) => b.hours * 60 + b.minutes - (a.hours * 60 + a.minutes),
    )
    setBusy(true)
    try {
      const result = await saveMilestoneAlerts(timerId, cleaned)
      setAlerts(result)
      setSaved(true)
    } catch {
      setError('Failed to save countdown alerts. Please try again.')
    } finally {
      setBusy(false)
    }
  }

  if (!alerts) {
    return (
      <>
        {error && <p className="error-message">{error}</p>}
        <p className="empty">Loading…</p>
      </>
    )
  }

  return (
    <form onSubmit={handleSave}>
      {error && <p className="error-message">{error}</p>}
      <div className="milestone-list">
        {alerts.map((a, i) => (
          <div className="control-group" key={i}>
            <label>
              Hours left
              <input
                type="number"
                min={0}
                step={1}
                value={a.hours}
                onChange={(e) => handleChange(i, 'hours', e.target.value)}
              />
            </label>
            <label>
              Minutes left
              <input
                type="number"
                min={0}
                max={59}
                step={1}
                value={a.minutes}
                onChange={(e) => handleChange(i, 'minutes', e.target.value)}
              />
            </label>
            <label>
              Sound
              <select
                value={a.sound}
                onChange={(e) =>
                  handleSoundChange(i, e.target.value as MilestoneAlert['sound'])
                }
              >
                {ALERT_SOUND_OPTIONS.map((opt) => (
                  <option key={opt.key} value={opt.key}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </label>
            <button type="button" onClick={() => playAlertSound(a.sound)}>
              ▶ Preview
            </button>
            <button
              type="button"
              className="link-button"
              onClick={() => handleRemove(i)}
            >
              Remove
            </button>
          </div>
        ))}
      </div>

      <div className="control-group">
        <button type="button" onClick={handleAdd}>
          Add alert
        </button>
        <button type="submit" disabled={busy}>
          {saved ? 'Saved!' : 'Save'}
        </button>
      </div>
    </form>
  )
}

/**
 * Full-page timer styling editor, moved out of the dashboard's control
 * grid so there's room for a live preview alongside the color pickers —
 * "WYSIWYG" in that the preview uses the exact same pill markup/CSS
 * classes as the real /overlay and /goals-overlay routes, just inline on
 * this page instead of full-screen. Presets (see PRESETS) are a quick
 * starting point; nothing is saved until Save is pressed, same pattern
 * as every other dashboard form.
 */
export default function Styling() {
  const { timerId } = useParams<{ timerId: string }>()
  const { snapshot } = useSubathon(timerId)
  const [milestones, setMilestones] = useState<MoneyMilestone[]>([])
  const [colors, setColors] = useState<OverlayColors | null>(null)
  const [icons, setIcons] = useState<StatIcons | null>(null)
  const [panelColors, setPanelColorsState] = useState<PanelColors | null>(null)
  const [explosionColor, setExplosionColorState] = useState<string | null>(null)
  const [leaderboard, setLeaderboard] = useState<Leaderboard | null>(null)
  const [previewDark, setPreviewDark] = useState(true)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [statsRotationBusy, setStatsRotationBusy] = useState(false)
  const [explosionBusy, setExplosionBusy] = useState(false)
  const [alertSoundsBusy, setAlertSoundsBusy] = useState(false)
  const [previewExploding, setPreviewExploding] = useState(false)

  // Seed the editable colors/icons once the timer's actual saved values
  // arrive over the websocket — only the first time, so this doesn't
  // blow away in-progress edits every time a new snapshot pushes down.
  // Adjusted during render (rather than a useEffect) since this is
  // really a synchronous derivation, same pattern as TwitchChannelSelect/
  // YouTubeChannelSelect on the Dashboard page.
  if (snapshot && colors === null) {
    setColors(snapshot.overlayColors)
  }
  if (snapshot && icons === null) {
    setIcons(snapshot.statIcons)
  }
  if (snapshot && panelColors === null) {
    setPanelColorsState(snapshot.panelColors)
  }
  if (snapshot && explosionColor === null) {
    setExplosionColorState(snapshot.explosionColor)
  }

  useEffect(() => {
    if (!timerId) return
    getMoneyMilestones(timerId)
      .then(setMilestones)
      .catch(() => setMilestones([]))
  }, [timerId])

  useEffect(() => {
    if (!timerId) return
    getLeaderboard(timerId)
      .then(setLeaderboard)
      .catch(() => setLeaderboard({ subs: [], bits: [], donations: [] }))
  }, [timerId])

  if (!timerId) {
    return (
      <div className="dashboard">
        <p>No timer ID in URL.</p>
        <Link to="/">Back to timers</Link>
      </div>
    )
  }

  const handleChange = (field: keyof OverlayColors, value: string) => {
    setColors((prev) => (prev ? { ...prev, [field]: value } : prev))
    setSaved(false)
  }

  const applyPreset = (preset: OverlayColors) => {
    setColors(preset)
    setSaved(false)
  }

  const handleSave = async () => {
    if (!colors) return
    setBusy(true)
    setError(null)
    setSaved(false)
    try {
      await setOverlayColors(timerId, colors)
      setSaved(true)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  // Unlike the color grid's batched Save, this toggle applies immediately
  // on change — same pattern as the Dashboard's Lock/Hide buttons — since
  // it's a single boolean rather than a multi-field form. The checkbox
  // reads straight off the live snapshot (not local-only state) so it
  // stays correct if another moderator toggles it elsewhere.
  const handleToggleStatsRotation = async (enabled: boolean) => {
    setStatsRotationBusy(true)
    setError(null)
    try {
      await setStatsRotation(timerId, enabled)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setStatsRotationBusy(false)
    }
  }

  // Same immediate-apply pattern as handleToggleStatsRotation above.
  const handleToggleExplosion = async (enabled: boolean) => {
    setExplosionBusy(true)
    setError(null)
    try {
      await setExplosion(timerId, enabled)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setExplosionBusy(false)
    }
  }

  const handleToggleAlertSounds = async (enabled: boolean) => {
    setAlertSoundsBusy(true)
    setError(null)
    try {
      await setAlertSounds(timerId, enabled)
    } catch (err) {
      setError(controlErrorMessage(err))
    } finally {
      setAlertSoundsBusy(false)
    }
  }

  const totalMoneyRaised = snapshot?.totalMoneyRaised ?? 0
  const previewMilestones = milestones.length > 0
    ? milestones.slice(0, 2)
    : [{ amount: 500, label: 'Sample goal', hidden: false }]

  return (
    <div className="dashboard dashboard-wide">
      <header>
        <div>
          <Link to={`/t/${timerId}`} className="back-link">
            &larr; {snapshot?.name ?? 'Timer'}
          </Link>
          <h1>Timer styling</h1>
        </div>
      </header>

      {error && <p className="error-message">{error}</p>}

      {colors === null || icons === null || panelColors === null || explosionColor === null ? (
        <p className="empty">Loading…</p>
      ) : (
        <>
          <section className="styling-preview-section">
            <div className="styling-preview-header">
              <h2>Preview</h2>
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={previewDark}
                  onChange={(e) => setPreviewDark(e.target.checked)}
                />
                Dark scene background
              </label>
              <button
                type="button"
                onClick={() => setPreviewExploding((prev) => !prev)}
              >
                {previewExploding ? 'Stop explosion preview' : 'Preview explosion'}
              </button>
            </div>
            <div
              className={
                previewDark
                  ? 'styling-preview-canvas styling-preview-canvas-dark'
                  : 'styling-preview-canvas styling-preview-canvas-light'
              }
            >
              <div
                className={previewExploding ? 'overlay-explosion-wrap' : undefined}
                style={previewExploding ? explosionColorStyle(explosionColor) : undefined}
              >
                {previewExploding && <ExplosionBurst color={explosionColor} />}
                <div
                  className={
                    previewExploding ? 'overlay-pill-group overlay-exploded' : 'overlay-pill-group'
                  }
                  style={{ background: colors.timerBg }}
                >
                  {snapshot?.statsRotationEnabled && (
                    <>
                      <RotatingStatPill
                        colors={colors}
                        icons={icons}
                        subsGiven={snapshot.subsGiven}
                        bitsGiven={snapshot.bitsGiven}
                        donationsGiven={snapshot.donationsGiven}
                      />
                      <div
                        className="overlay-pill-divider"
                        style={{ background: colors.timerText }}
                      />
                    </>
                  )}
                  {snapshot?.moneyGoal ? (
                    <>
                      <div
                        className="overlay-pill"
                        style={{ background: colors.moneyBg, color: colors.moneyText }}
                      >
                        <span className="overlay-money-label">
                          ${formatWholeMoney(totalMoneyRaised)} / $
                          {formatWholeMoney(snapshot.moneyGoal)}
                        </span>
                      </div>
                      <div
                        className="overlay-pill-divider"
                        style={{ background: colors.timerText }}
                      />
                    </>
                  ) : null}
                  <div
                    className="overlay-pill"
                    style={{ background: colors.timerBg, color: colors.timerText }}
                  >
                    <span className="overlay-clock">
                      {snapshot ? formatDuration(snapshot.remainingSecs) : '--:--:--'}
                    </span>
                  </div>
                </div>
              </div>
              <div className="overlay-goals-list">
                {previewMilestones.map((m) => {
                  const reached = totalMoneyRaised >= m.amount
                  return (
                    <div
                      key={m.amount}
                      className="overlay-goal-pill"
                      style={{ background: colors.goalBg, color: colors.goalText }}
                    >
                      <span
                        className="overlay-goal-amount-pill"
                        style={{
                          background: colors.goalAmountBg,
                          color: colors.goalAmountText,
                        }}
                      >
                        {reached ? '✓' : `$${formatWholeMoney(m.amount)}`}
                      </span>
                      <span className="overlay-goal-label">{m.label}</span>
                    </div>
                  )
                })}
              </div>
            </div>
            <p className="control-hint">
              This is exactly the markup the /overlay and /goals-overlay
              browser sources render — what you see here is what shows up
              in OBS.
            </p>
          </section>

          <section className="styling-presets-section">
            <h2>Presets</h2>
            <div className="styling-presets-grid">
              {PRESETS.map((preset) => (
                <button
                  key={preset.name}
                  type="button"
                  className="preset-button"
                  onClick={() => applyPreset(preset.colors)}
                >
                  <PresetSwatch colors={preset.colors} />
                  {preset.name}
                </button>
              ))}
            </div>
          </section>

          <section className="styling-stats-section">
            <h2>Rotating stat list</h2>
            <div className="control-group control-group-stacked">
              <div className="control-row">
                <div className="control-toggle">
                  <span
                    className={`status ${snapshot?.statsRotationEnabled ? 'status-ok' : 'status-down'}`}
                  >
                    {snapshot?.statsRotationEnabled ? 'Shown' : 'Hidden'}
                  </span>
                  <span className="control-hint">
                    A pill on the far left of the overlay that cycles
                    through total subs, bits/Kicks, and tips/donations
                    given, one icon at a time.
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() =>
                    handleToggleStatsRotation(!snapshot?.statsRotationEnabled)
                  }
                  disabled={statsRotationBusy || !snapshot}
                >
                  {snapshot?.statsRotationEnabled ? 'Hide' : 'Show'}
                </button>
              </div>
            </div>
            {snapshot && (
              <>
                <StatIconsForm
                  icons={icons}
                  onChange={setIcons}
                  onSave={() => setStatIcons(timerId, icons)}
                />
                <ContributionCountsForm
                  initialCounts={{
                    subsGiven: snapshot.subsGiven,
                    bitsGiven: snapshot.bitsGiven,
                    donationsGiven: snapshot.donationsGiven,
                  }}
                  onSave={(counts) => setContributionCounts(timerId, counts)}
                />
              </>
            )}
          </section>

          <section className="styling-stats-section">
            <h2>Timer finale</h2>
            <div className="control-group control-group-stacked">
              <div className="control-row">
                <div className="control-toggle">
                  <span
                    className={`status ${snapshot?.explosionEnabled ? 'status-ok' : 'status-down'}`}
                  >
                    {snapshot?.explosionEnabled ? 'Shown' : 'Hidden'}
                  </span>
                  <span className="control-hint">
                    When the clock hits 00:00:00, the timer pill bursts
                    apart instead of just sitting at zero — a flash,
                    flying debris, and lingering embers. Use the preview
                    button above to see it without waiting for the clock.
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => handleToggleExplosion(!snapshot?.explosionEnabled)}
                  disabled={explosionBusy || !snapshot}
                >
                  {snapshot?.explosionEnabled ? 'Hide' : 'Show'}
                </button>
              </div>
            </div>
            <ExplosionColorForm
              color={explosionColor}
              onChange={setExplosionColorState}
              onSave={() => setExplosionColor(timerId, explosionColor)}
            />
          </section>

          <section className="styling-stats-section">
            <h2>Countdown alerts</h2>
            <div className="control-group control-group-stacked">
              <div className="control-row">
                <div className="control-toggle">
                  <span
                    className={`status ${snapshot?.alertSoundsEnabled ? 'status-ok' : 'status-down'}`}
                  >
                    {snapshot?.alertSoundsEnabled ? 'On' : 'Off'}
                  </span>
                  <span className="control-hint">
                    Master switch for every alert below — plays its
                    chosen sound on the overlay page itself (not through
                    the dashboard) each time the clock counts down past
                    one of its configured marks.
                  </span>
                </div>
                <button
                  type="button"
                  onClick={() => handleToggleAlertSounds(!snapshot?.alertSoundsEnabled)}
                  disabled={alertSoundsBusy || !snapshot}
                >
                  {snapshot?.alertSoundsEnabled ? 'Turn off' : 'Turn on'}
                </button>
              </div>
            </div>
            <MilestoneAlertsEditor timerId={timerId} />
          </section>

          <section className="controls">
            <form
              className="control-group control-group-stacked"
              onSubmit={(e) => {
                e.preventDefault()
                handleSave()
              }}
            >
              <h2 className="control-group-heading">Timer &amp; money pills</h2>
              <div className="overlay-colors-grid">
                <label>
                  Timer pill background
                  <input
                    type="color"
                    value={colors.timerBg}
                    onChange={(e) => handleChange('timerBg', e.target.value)}
                  />
                </label>
                <label>
                  Timer pill text
                  <input
                    type="color"
                    value={colors.timerText}
                    onChange={(e) => handleChange('timerText', e.target.value)}
                  />
                </label>
                <label>
                  Money pill background
                  <input
                    type="color"
                    value={colors.moneyBg}
                    onChange={(e) => handleChange('moneyBg', e.target.value)}
                  />
                </label>
                <label>
                  Money pill text
                  <input
                    type="color"
                    value={colors.moneyText}
                    onChange={(e) => handleChange('moneyText', e.target.value)}
                  />
                </label>
              </div>

              <h2 className="control-group-heading">Goal pills</h2>
              <div className="overlay-colors-grid">
                <label>
                  Goal pill background
                  <input
                    type="color"
                    value={colors.goalBg}
                    onChange={(e) => handleChange('goalBg', e.target.value)}
                  />
                </label>
                <label>
                  Goal pill text
                  <input
                    type="color"
                    value={colors.goalText}
                    onChange={(e) => handleChange('goalText', e.target.value)}
                  />
                </label>
                <label>
                  Goal amount pill background
                  <input
                    type="color"
                    value={colors.goalAmountBg}
                    onChange={(e) => handleChange('goalAmountBg', e.target.value)}
                  />
                </label>
                <label>
                  Goal amount pill text
                  <input
                    type="color"
                    value={colors.goalAmountText}
                    onChange={(e) => handleChange('goalAmountText', e.target.value)}
                  />
                </label>
              </div>

              <button type="submit" disabled={busy}>
                {saved ? 'Saved!' : 'Save styling'}
              </button>
            </form>
          </section>

          <section className="styling-panel-section">
            <h2>Leaderboard panel</h2>
            <p className="control-hint">
              A separate page showing the top 10 contributors in each
              category — screenshot it for a Twitch panel image (Twitch's
              own panels only support a static image plus a link, not a
              live embed), add it as its own OBS browser source, or share
              the link directly (see its URL on the timer page).
            </p>
            <div
              className="styling-panel-preview"
              style={{ background: panelColors.bg, color: panelColors.text }}
            >
              <h3 className="panel-title">{snapshot?.name ?? 'Top Contributors'}</h3>
              {!leaderboard ? (
                <p className="panel-loading">Loading…</p>
              ) : (
                <div className="panel-categories">
                  {STAT_CATEGORIES.map((cat) => {
                    const entries = leaderboard[cat.iconKey]
                    return (
                      <section key={cat.iconKey} className="panel-category">
                        <div
                          className="panel-category-heading"
                          style={{
                            background: panelColors.accentBg,
                            color: panelColors.accentText,
                          }}
                        >
                          <CategoryIcon
                            icons={icons}
                            category={cat}
                            className="panel-category-icon"
                          />
                          {cat.label}
                        </div>
                        {entries.length === 0 ? (
                          <p className="panel-empty">No contributions yet.</p>
                        ) : (
                          <ol className="panel-list">
                            {entries.map((entry, i) => (
                              <li key={entry.username}>
                                <span className="panel-rank">{i + 1}</span>
                                <span className="panel-username">{entry.username}</span>
                                <span className="panel-amount">
                                  {cat.countPrefix}
                                  {formatCompactCount(entry.amount)}
                                </span>
                              </li>
                            ))}
                          </ol>
                        )}
                      </section>
                    )
                  })}
                </div>
              )}
            </div>
            <p className="control-hint">
              This is exactly the markup the /panel page renders — what
              you see here is what shows up there.
            </p>
            <PanelColorsForm
              colors={panelColors}
              onChange={setPanelColorsState}
              onSave={() => setPanelColors(timerId, panelColors)}
            />
          </section>
        </>
      )}
    </div>
  )
}
