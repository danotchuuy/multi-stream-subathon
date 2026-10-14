import { useEffect, useRef } from 'react'
import type { AlertSoundKey, MilestoneAlert, Snapshot } from '../types'

/** Mirrors subathon.DefaultAlertSound. */
export const DEFAULT_ALERT_SOUND: AlertSoundKey = 'chime'

let sharedCtx: AudioContext | null = null

/** Lazily creates (and reuses) a single AudioContext for the page's
 * lifetime — browsers cap how many can exist, and there's no reason to
 * make a new one per chime. Returns null in an environment with no Web
 * Audio support (or during server-side rendering, which this app
 * doesn't do, but costs nothing to guard against). */
function getAudioContext(): AudioContext | null {
  if (typeof window === 'undefined') return null
  const Ctor =
    window.AudioContext ||
    (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!Ctor) return null
  if (!sharedCtx) sharedCtx = new Ctor()
  return sharedCtx
}

/** Schedules a single tone with a quick fade-in and exponential
 * fade-out, so it reads as a "note" rather than a harsh on/off beep or
 * an audible click at either end. */
function tone(
  ctx: AudioContext,
  freq: number,
  start: number,
  duration: number,
  type: OscillatorType = 'sine',
  peakGain = 0.4,
) {
  const osc = ctx.createOscillator()
  const gain = ctx.createGain()
  osc.type = type
  osc.frequency.setValueAtTime(freq, start)
  gain.gain.setValueAtTime(0, start)
  gain.gain.linearRampToValueAtTime(peakGain, start + 0.02)
  gain.gain.exponentialRampToValueAtTime(0.001, start + duration)
  osc.connect(gain)
  gain.connect(ctx.destination)
  osc.start(start)
  osc.stop(start + duration + 0.02)
}

/** Like tone(), but holds at peakGain for most of the duration instead
 * of decaying the whole way through — reads as a sustained, held note
 * rather than a struck one, which suits a horn blast better than tone()'s
 * continuous decay. */
function heldTone(
  ctx: AudioContext,
  freq: number,
  start: number,
  duration: number,
  type: OscillatorType = 'sawtooth',
  peakGain = 0.2,
) {
  const osc = ctx.createOscillator()
  const gain = ctx.createGain()
  osc.type = type
  osc.frequency.setValueAtTime(freq, start)
  gain.gain.setValueAtTime(0, start)
  gain.gain.linearRampToValueAtTime(peakGain, start + 0.03)
  gain.gain.setValueAtTime(peakGain, start + Math.max(0.03, duration - 0.08))
  gain.gain.exponentialRampToValueAtTime(0.001, start + duration)
  osc.connect(gain)
  gain.connect(ctx.destination)
  osc.start(start)
  osc.stop(start + duration + 0.02)
}

/** Like tone(), but sweeps frequency linearly from `from` to `to` over
 * the tone's duration instead of holding one pitch — used for the siren. */
function sweep(
  ctx: AudioContext,
  from: number,
  to: number,
  start: number,
  duration: number,
  type: OscillatorType = 'sine',
  peakGain = 0.4,
) {
  const osc = ctx.createOscillator()
  const gain = ctx.createGain()
  osc.type = type
  osc.frequency.setValueAtTime(from, start)
  osc.frequency.linearRampToValueAtTime(to, start + duration)
  gain.gain.setValueAtTime(0, start)
  gain.gain.linearRampToValueAtTime(peakGain, start + 0.03)
  gain.gain.setValueAtTime(peakGain, start + duration - 0.05)
  gain.gain.exponentialRampToValueAtTime(0.001, start + duration)
  osc.connect(gain)
  gain.connect(ctx.destination)
  osc.start(start)
  osc.stop(start + duration + 0.02)
}

/** One built-in alert sound: how it's labeled in the styling page's
 * picker, and how to schedule it on a given AudioContext starting now.
 * Every entry is synthesized (sine/triangle/sawtooth oscillators), so
 * adding a sound here needs no audio asset. */
const ALERT_SOUND_PLAYERS: Record<AlertSoundKey, (ctx: AudioContext) => void> = {
  // A soft, friendly two-note "ding-dong".
  chime: (ctx) => {
    const now = ctx.currentTime
    tone(ctx, 880, now, 0.18)
    tone(ctx, 1320, now + 0.16, 0.22)
  },
  // Three short, urgent beeps.
  alarm: (ctx) => {
    const now = ctx.currentTime
    for (let i = 0; i < 3; i++) {
      tone(ctx, 1000, now + i * 0.18, 0.12, 'square', 0.25)
    }
  },
  // A single low, punchy blast.
  horn: (ctx) => {
    const now = ctx.currentTime
    tone(ctx, 220, now, 0.5, 'sawtooth', 0.3)
    tone(ctx, 221, now, 0.5, 'sawtooth', 0.3)
  },
  // A bell-like ding with a couple of harmonic overtones and a long
  // decay, instead of a single flat sine tone.
  bell: (ctx) => {
    const now = ctx.currentTime
    tone(ctx, 988, now, 1.1, 'sine', 0.35)
    tone(ctx, 1976, now, 0.8, 'sine', 0.12)
    tone(ctx, 2960, now, 0.5, 'sine', 0.06)
  },
  // A classic rising/falling siren sweep.
  siren: (ctx) => {
    const now = ctx.currentTime
    sweep(ctx, 500, 1000, now, 0.4, 'triangle', 0.3)
    sweep(ctx, 1000, 500, now + 0.4, 0.4, 'triangle', 0.3)
  },
  // A mournful civil-defense wail — three full up/down cycles, each
  // noticeably slower and pitched higher than the quick siren above, so
  // the two read as distinctly different alerts rather than variations
  // on one.
  tornadoSiren: (ctx) => {
    const now = ctx.currentTime
    const cycle = 1.8
    for (let i = 0; i < 3; i++) {
      const start = now + i * cycle
      sweep(ctx, 600, 950, start, cycle / 2, 'triangle', 0.32)
      sweep(ctx, 950, 600, start + cycle / 2, cycle / 2, 'triangle', 0.32)
    }
  },
  // A dissonant, held chord (real diesel horns chime several close
  // pitches at once), honked twice like a grade-crossing signal.
  trainHorn: (ctx) => {
    const now = ctx.currentTime
    const chord = [311.1, 370.0, 415.3, 554.4]
    const blast = 0.5
    const gap = 0.12
    for (const offset of [0, blast + gap]) {
      for (const freq of chord) {
        heldTone(ctx, freq, now + offset, blast, 'sawtooth', 0.16)
      }
    }
  },
}

/** The styling page's sound picker options, in display order. */
export const ALERT_SOUND_OPTIONS: { key: AlertSoundKey; label: string }[] = [
  { key: 'chime', label: 'Chime' },
  { key: 'alarm', label: 'Alarm' },
  { key: 'horn', label: 'Horn' },
  { key: 'bell', label: 'Bell' },
  { key: 'siren', label: 'Siren' },
  { key: 'tornadoSiren', label: 'Tornado siren' },
  { key: 'trainHorn', label: 'Train horn' },
]

/**
 * Plays one of the built-in alert sounds, synthesized with the Web Audio
 * API (no audio file needed). Safe to call from a user gesture (e.g.
 * Styling.tsx's per-alert preview button) or not — if the browser's
 * autoplay policy has the shared AudioContext suspended, this tries to
 * resume it and simply does nothing audible if that fails, rather than
 * throwing. Falls back to DEFAULT_ALERT_SOUND for an unrecognized key.
 */
export function playAlertSound(sound: AlertSoundKey) {
  const ctx = getAudioContext()
  if (!ctx) return

  if (ctx.state === 'suspended') {
    ctx.resume().catch(() => {
      // Autoplay was blocked outside a user gesture — nothing more to
      // do; the next call (e.g. a preview button click) may succeed.
    })
  }

  const play = ALERT_SOUND_PLAYERS[sound] ?? ALERT_SOUND_PLAYERS[DEFAULT_ALERT_SOUND]
  play(ctx)
}

/**
 * Plays each configured alert's sound once each time
 * snapshot.remainingSecs crosses down through its remaining-time mark
 * (see MilestoneAlert.hours/minutes), as long as
 * snapshot.alertSoundsEnabled is on. Does nothing on the very first
 * snapshot a page receives (so reloading the overlay mid-stream, already
 * under a mark, doesn't immediately blast every earlier alert at once)
 * — only an actual downward crossing while mounted triggers one. If the
 * clock gets reset or extended back above a mark, that alert's "has it
 * fired yet" flag clears, so the next countdown through it chimes again
 * rather than staying silent for the rest of the timer's life.
 */
export function useMilestoneAlertSounds(
  snapshot: Snapshot | null | undefined,
  alerts: MilestoneAlert[],
) {
  const prevRemainingRef = useRef<number | null>(null)
  const firedRef = useRef<Set<number>>(new Set())

  useEffect(() => {
    if (!snapshot) return
    const prev = prevRemainingRef.current
    const curr = snapshot.remainingSecs

    if (snapshot.alertSoundsEnabled && prev !== null) {
      alerts.forEach((alert, i) => {
        const threshold = alert.hours * 3600 + alert.minutes * 60
        if (curr > threshold) {
          // Time was added back past this mark; let it chime again next
          // time the clock counts down through it.
          firedRef.current.delete(i)
        } else if (prev > threshold && !firedRef.current.has(i)) {
          firedRef.current.add(i)
          playAlertSound(alert.sound)
        }
      })
    }

    prevRemainingRef.current = curr
    // snapshot is a new object every broadcast tick (see useSubathon),
    // so depending on it directly is equivalent to depending on its
    // individual fields but doesn't need its own eslint-disable.
  }, [snapshot, alerts])
}
