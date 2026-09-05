import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useSubathon } from '../lib/useSubathon'
import {
  getMoneyMilestones,
  setContributionCounts,
  setOverlayColors,
  setStatIcons,
  setStatsRotation,
  HttpError,
} from '../lib/api'
import { formatMoney, formatWholeMoney } from '../lib/format'
import {
  DEFAULT_ICON_VALUES,
  EMOJI_OPTIONS,
  RotatingStatPill,
  STAT_CATEGORIES,
  StatSvgIcon,
  SVG_ICON_OPTIONS,
} from '../lib/statRotation'
import type { MoneyMilestone, OverlayColors, StatIconStyle, StatIcons } from '../types'

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
  const [previewDark, setPreviewDark] = useState(true)
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [statsRotationBusy, setStatsRotationBusy] = useState(false)

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

  useEffect(() => {
    if (!timerId) return
    getMoneyMilestones(timerId)
      .then(setMilestones)
      .catch(() => setMilestones([]))
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

      {colors === null || icons === null ? (
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
            </div>
            <div
              className={
                previewDark
                  ? 'styling-preview-canvas styling-preview-canvas-dark'
                  : 'styling-preview-canvas styling-preview-canvas-light'
              }
            >
              <div className="overlay-pill-group" style={{ background: colors.timerBg }}>
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
                  <span className="overlay-clock">02:14:37</span>
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
                        {reached ? '✓' : `$${formatMoney(m.amount)}`}
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
        </>
      )}
    </div>
  )
}
