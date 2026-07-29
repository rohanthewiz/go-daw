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
