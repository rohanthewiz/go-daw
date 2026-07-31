package audio

import (
	"math"
	"sync/atomic"
)

// Click flavors queued by TriggerClick. Zero doubles as "nothing pending" so
// the callback's Swap(0) reads and clears the mailbox in one wait-free step.
const (
	clickNone   int32 = 0
	clickTick   int32 = 1
	clickAccent int32 = 2
)

// Metronome voicing. Two pitches a fifth-and-change apart read instantly as
// "bar start" vs "beat" even at low volume; the accent is also a touch
// louder. Amplitudes are conservative because the click mixes in after the
// master limiter (see Render) and must bound itself.
const (
	clickTickHz    = 1175.0 // D6
	clickAccentHz  = 1760.0 // A6
	clickTickAmp   = 0.30
	clickAccentAmp = 0.45
	clickSeconds   = 0.030 // burst length; short enough to never smear beats
)

// clickGen synthesizes the metronome tick: a sine burst with an exponential
// decay, i.e. exactly the "oscillator with an envelope" a hardware metronome
// is. It is not a mixer source on purpose — the click is a monitoring aid
// tied to the engine, not a channel the user patches, mutes, or records.
//
// Realtime contract: pending is the only cross-thread field (control plane
// stores, audio thread swaps); everything below it is owned exclusively by
// the audio thread once Render starts getting called.
type clickGen struct {
	pending atomic.Int32 // clickNone / clickTick / clickAccent mailbox

	phase     float64 // 0..1 cycle position within the burst
	inc       float64 // per-sample phase increment for the burst's pitch
	amp       float64 // current envelope level, decays toward zero
	decay     float64 // per-sample envelope multiplier (precomputed)
	remaining int     // samples left in the burst; 0 = silent

	sampleRate float64
}

// newClickGen precomputes the envelope constant: decay^N must take the
// burst from full level down ~60dB (inaudible) across clickSeconds, which
// keeps the tail click-free without a separate release stage.
func newClickGen(sampleRate float64) *clickGen {
	n := clickSeconds * sampleRate
	return &clickGen{
		decay:      math.Exp(math.Log(0.001) / n),
		sampleRate: sampleRate,
	}
}

// Trigger queues one click. Control plane; wait-free for the audio thread.
// If two triggers land within one block only the last wins — at any musical
// tempo clicks are hundreds of ms apart, so coalescing is harmless.
func (c *clickGen) Trigger(accent bool) {
	if accent {
		c.pending.Store(clickAccent)
	} else {
		c.pending.Store(clickTick)
	}
}

// Render mixes the click into an interleaved stereo buffer of n frames.
// Audio thread only. A pending trigger (re)starts the burst at phase zero;
// retrigger-during-decay just restarts, which is inaudible at click length.
//
// The output is clamped per sample: this runs after the master limiter (so
// clicks stay out of recordings), which means nothing downstream will catch
// an overshoot — the click bounds its own sum with the program material.
func (c *clickGen) Render(out []float32, n int) {
	if p := c.pending.Swap(clickNone); p != clickNone {
		hz, amp := clickTickHz, clickTickAmp
		if p == clickAccent {
			hz, amp = clickAccentHz, clickAccentAmp
		}
		c.phase = 0
		c.inc = hz / c.sampleRate
		c.amp = amp
		c.remaining = int(clickSeconds * c.sampleRate)
	}
	if c.remaining == 0 {
		return
	}
	for i := 0; i < n && c.remaining > 0; i++ {
		v := float32(math.Sin(2*math.Pi*c.phase) * c.amp)
		c.phase += c.inc
		if c.phase >= 1 {
			c.phase--
		}
		c.amp *= c.decay
		c.remaining--

		out[i*2] = clampUnit(out[i*2] + v)
		out[i*2+1] = clampUnit(out[i*2+1] + v)
	}
}

func clampUnit(v float32) float32 {
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}
