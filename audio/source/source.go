// Package source defines where a mixer channel's audio comes from: a live
// capture feed, a WAV file preloaded into memory, or a test oscillator.
package source

// Source produces one block of stereo audio. Read fills l and r (equal
// length) and returns how many frames it actually wrote; the engine
// zero-fills the remainder, so end-of-file yields silence rather than
// stale buffer garbage.
//
// Read is called on the audio thread: implementations must not allocate,
// lock, log, or touch disk.
type Source interface {
	Read(l, r []float32) int
	Name() string // human label for the UI, e.g. "osc", "wav:kick.wav", "live"
}

// NotePlayer is the note-event surface shared by playable sources (PolySynth,
// SFSynth). The piano UI and tutorial target this interface rather than a
// concrete synth, so swapping the additive synth for a SoundFont bank — or
// any future instrument — needs no handler changes. Both methods are control
// plane: implementations queue into lock-free rings, never touching the audio
// thread directly.
type NotePlayer interface {
	NoteOn(note int, vel float64) // velocity 0..1
	NoteOff(note int)
}

// Sustainer is the damper-pedal surface (MIDI CC64). Kept separate from
// NotePlayer so a future instrument without pedal semantics (a drum machine,
// say) can still be played; handlers feature-detect with a type assertion.
// Control plane, same ring discipline as note events.
type Sustainer interface {
	Sustain(down bool)
}
