package dsp

import (
	"math"
	"sync/atomic"
)

// Reverb is a Schroeder/Freeverb-style reverberator with a configurable
// pre-delay, run as a per-channel insert with a wet/dry Mix control.
//
// Topology per side:
//
//	          ┌────────────┐   ┌──────┐
//	 in ──┬──►│ pre-delay  │──►│comb 1│─┐
//	      │   │ (circular) │   ├──────┤ │
//	      │   └────────────┘──►│comb 2│─┤   ┌────────┐  ┌────────┐
//	      │                 ──►│comb 3│─┼──►│allpass1│─►│allpass2│──► wet
//	      │                 ──►│comb 4│─┘·¼ └────────┘  └────────┘
//	      │                    └──────┘
//	      └────────────────────────── dry ──────────► out = dry·(1-mix)+wet·mix
//
// Four parallel lowpass-feedback combs build the dense tail (their mutually
// prime lengths keep resonant modes from stacking into metallic ringing);
// two series allpasses smear the echoes into diffusion. The pre-delay is a
// plain circular buffer read N samples behind the write head — perceptually
// it separates the dry transient from the reverb onset, mimicking the time
// sound takes to reach the walls of a room.
type Reverb struct {
	Enabled    atomic.Bool
	PreDelayMs *ParamCell // 0..250 ms
	Decay      *ParamCell // 0..1 -> comb feedback (tail length)
	Damp       *ParamCell // 0..1 high-frequency absorption in the comb loop
	Mix        *ParamCell // 0 = fully dry, 1 = fully wet

	pre   [2]delayLine   // pre-delay per side
	combs [2][4]combFilter
	aps   [2][2]allpassFilter

	sampleRate float64
}

// Freeverb's classic tunings, in samples at 44.1kHz. Scaled to the engine
// rate at construction. The right channel adds stereoSpread to every length —
// the cheap trick that decorrelates the two tails into believable width
// without doubling the topology.
var combTunings = [4]int{1116, 1188, 1277, 1356}
var allpassTunings = [2]int{556, 441}

const stereoSpread = 23
const maxPreDelayMs = 250

type delayLine struct {
	buf []float32
	w   int // write index
}

// combFilter is a comb with a one-pole lowpass in its feedback loop
// (the "lp" state) — each pass around the loop loses treble, darkening the
// tail over time the way air absorption does in a real room.
type combFilter struct {
	buf []float32
	idx int
	lp  float64
}

func (c *combFilter) process(x, feedback, damp float64) float64 {
	y := float64(c.buf[c.idx])
	c.lp = y*(1-damp) + c.lp*damp
	c.buf[c.idx] = float32(x + c.lp*feedback)
	c.idx++
	if c.idx >= len(c.buf) {
		c.idx = 0
	}
	return y
}

// allpassFilter passes all frequencies at equal gain but scrambles their
// phase, turning discrete comb echoes into diffuse reverberation.
type allpassFilter struct {
	buf []float32
	idx int
}

func (a *allpassFilter) process(x float64) float64 {
	const g = 0.5
	buffered := float64(a.buf[a.idx])
	y := buffered - x
	a.buf[a.idx] = float32(x + buffered*g)
	a.idx++
	if a.idx >= len(a.buf) {
		a.idx = 0
	}
	return y
}

// NewReverb sizes all buffers for the given sample rate. Buffers are
// allocated once here — never on the audio thread.
func NewReverb(sampleRate float64) *Reverb {
	r := &Reverb{
		PreDelayMs: NewParam(20),
		Decay:      NewParam(0.5),
		Damp:       NewParam(0.4),
		Mix:        NewParam(0.25),
		sampleRate: sampleRate,
	}
	scale := sampleRate / 44100.0
	preCap := int(maxPreDelayMs*0.001*sampleRate) + 1

	for side := 0; side < 2; side++ {
		spread := side * stereoSpread
		r.pre[side] = delayLine{buf: make([]float32, preCap)}
		for i, t := range combTunings {
			n := int(float64(t+spread)*scale + 0.5)
			r.combs[side][i] = combFilter{buf: make([]float32, n)}
		}
		for i, t := range allpassTunings {
			n := int(float64(t+spread)*scale + 0.5)
			r.aps[side][i] = allpassFilter{buf: make([]float32, n)}
		}
	}
	return r
}

// ProcessBlock runs the reverb in place. Audio thread only.
func (r *Reverb) ProcessBlock(l, ri []float32) {
	if !r.Enabled.Load() {
		return
	}

	mix := r.Mix.Get()
	damp := r.Damp.Get()
	// Map Decay 0..1 onto comb feedback 0.7..0.98: below 0.7 the tail is
	// too short to read as reverb, above 0.98 it approaches self-oscillation.
	feedback := 0.7 + 0.28*clamp01(r.Decay.Get())

	preSamples := int(r.PreDelayMs.Get() * 0.001 * r.sampleRate)
	if max := len(r.pre[0].buf) - 1; preSamples > max {
		preSamples = max
	}

	sides := [2][]float32{l, ri}
	for side := 0; side < 2; side++ {
		buf := sides[side]
		pre := &r.pre[side]
		for i, x := range buf {
			dry := float64(x)

			// Pre-delay: write now, read preSamples behind. A live change of
			// pre-delay just moves the read offset (a small click is accepted).
			pre.buf[pre.w] = x
			readIdx := pre.w - preSamples
			if readIdx < 0 {
				readIdx += len(pre.buf)
			}
			delayed := float64(pre.buf[readIdx])
			pre.w++
			if pre.w >= len(pre.buf) {
				pre.w = 0
			}

			// Parallel combs, averaged (·¼ keeps unity-ish loop gain).
			wet := 0.0
			for c := range r.combs[side] {
				wet += r.combs[side][c].process(delayed, feedback, damp)
			}
			wet *= 0.25

			// Series allpass diffusion.
			for a := range r.aps[side] {
				wet = r.aps[side][a].process(wet)
			}

			buf[i] = float32(dry*(1-mix) + wet*mix)
		}
	}
}

func clamp01(x float64) float64 {
	return math.Min(1, math.Max(0, x))
}
