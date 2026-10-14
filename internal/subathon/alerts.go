package subathon

// AlertSound is one of the built-in chime presets a MilestoneAlert can
// play. The server only stores which one was picked — the sounds
// themselves are synthesized client-side (see frontend lib/alertSound.ts)
// rather than shipped as audio files.
type AlertSound string

const (
	AlertSoundChime        AlertSound = "chime"
	AlertSoundAlarm        AlertSound = "alarm"
	AlertSoundHorn         AlertSound = "horn"
	AlertSoundBell         AlertSound = "bell"
	AlertSoundSiren        AlertSound = "siren"
	AlertSoundTornadoSiren AlertSound = "tornadoSiren"
	AlertSoundTrainHorn    AlertSound = "trainHorn"
)

// DefaultAlertSound is used for a MilestoneAlert saved with an empty or
// unrecognized Sound.
const DefaultAlertSound = AlertSoundChime

// validAlertSounds backs IsValidAlertSound.
var validAlertSounds = map[AlertSound]bool{
	AlertSoundChime:        true,
	AlertSoundAlarm:        true,
	AlertSoundHorn:         true,
	AlertSoundBell:         true,
	AlertSoundSiren:        true,
	AlertSoundTornadoSiren: true,
	AlertSoundTrainHorn:    true,
}

// IsValidAlertSound reports whether s is one of the built-in presets
// above.
func IsValidAlertSound(s AlertSound) bool {
	return validAlertSounds[s]
}

// MilestoneAlert is one "play a sound when this much time is left"
// countdown alert for the main overlay (see Timer.MilestoneAlerts) —
// Hours/Minutes together give the remaining-time mark it fires at (e.g.
// Hours: 1, Minutes: 30 fires at 1h30m left), and Sound picks which
// built-in chime plays. A timer can have any number of these; they all
// share Timer.AlertSoundsEnabled as a single master on/off switch rather
// than each needing its own.
type MilestoneAlert struct {
	Hours   int        `json:"hours"`
	Minutes int        `json:"minutes"`
	Sound   AlertSound `json:"sound"`
}

// RemainingSecs returns the countdown mark (in seconds) this alert fires
// at.
func (a MilestoneAlert) RemainingSecs() int {
	return a.Hours*3600 + a.Minutes*60
}
