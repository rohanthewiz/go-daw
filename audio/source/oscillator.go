package source

import (
	"math"
	"sync/atomic"

	"github.com/rohanthewiz/go-daw/dsp"
)

// Waveform selects the oscillator shape.
type Waveform int32

const (
	WaveSine Waveform = iota
	WaveSquare
)

// Oscillator is a phase-accumulator test-tone generator. It exists so the
// mixer makes sound out of the box — no files, no microphone — which makes
// every downstream feature (EQ, compressor, meters, recording) immediately
// auditable by ear.
type Oscillator struct {
	FreqHz  *dsp.ParamCell // 20..20k
	LevelDB *dsp.ParamCell // output level in dBFS
	wave    atomic.Int32

	phase      float64 // 0..1 cycle position; accumulator survives blocks so there are no phase clicks
	sampleRate float64
}

// NewOscillator returns a sine at the given frequency/level.
func NewOscillator(sampleRate, freqHz, levelDB float64) *Oscillator {
	return &Oscillator{
		FreqHz:     dsp.NewParam(freqHz),
		LevelDB:    dsp.NewParam(levelDB),
		sampleRate: sampleRate,
	}
}

// SetWave switches the waveform (control plane).
func (o *Oscillator) SetWave(w Waveform) { o.wave.Store(int32(w)) }

// Wave returns the current waveform.
func (o *Oscillator) Wave() Waveform { return Waveform(o.wave.Load()) }

// Read synthesizes one block. Audio thread.
func (o *Oscillator) Read(l, r []float32) int {
	inc := o.FreqHz.Get() / o.sampleRate
	amp := dsp.DBToLin(o.LevelDB.Get())
	square := Waveform(o.wave.Load()) == WaveSquare

	for i := range l {
		s := math.Sin(2 * math.Pi * o.phase)
		if square {
			// Sign of the sine gives a square with the same period. Naive
			// (aliased) square is fine for a test tone.
			if s >= 0 {
				s = 1
			} else {
				s = -1
			}
		}
		v := float32(s * amp)
		l[i], r[i] = v, v

		o.phase += inc
		if o.phase >= 1 {
			o.phase -= 1
		}
	}
	return len(l)
}

func (o *Oscillator) Name() string { return "osc" }
