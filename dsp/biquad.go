package dsp

import (
	"math"
	"sync/atomic"
)

// FilterKind selects which RBJ cookbook response a Biquad realizes.
type FilterKind int

const (
	FilterPeaking FilterKind = iota // parametric EQ band (boost/cut at f0)
	FilterLowPass
	FilterHighPass
)

// BiquadCoeffs is one immutable, already-normalized coefficient set
// (divided through by a0). It is published to the audio thread as a whole
// struct behind an atomic pointer because the five values only make sense
// together — publishing them one at a time could momentarily mix halves of
// two different filter responses and produce a loud transient.
type BiquadCoeffs struct {
	B0, B1, B2, A1, A2 float64
}

// Biquad is a second-order IIR filter in Transposed Direct Form II.
//
//	y[n]  = b0*x[n] + z1
//	z1    = b1*x[n] - a1*y[n] + z2
//	z2    = b2*x[n] - a2*y[n]
//
// TDF-II is chosen over DF-I for its lower state count (2 vs 4) and better
// numerical behavior with time-varying coefficients.
//
// State and coefficients are float64 even though the audio path is float32:
// low-frequency, high-Q biquads are numerically fragile — float32 coefficient
// quantization shifts poles enough to be audible near DC. Doubling the state
// costs nothing measurable and removes that entire class of bug.
//
// The struct carries the user-facing parameters (freq/gain/Q) alongside the
// derived coefficients so a scene snapshot can read them back directly.
type Biquad struct {
	Kind    FilterKind
	Enabled atomic.Bool

	// User parameters (control plane reads/writes; Update derives coeffs).
	Freq   *ParamCell // center / cutoff frequency in Hz
	GainDB *ParamCell // peaking bands only
	Q      *ParamCell

	coeffs atomic.Pointer[BiquadCoeffs]

	// Per-side filter state. One Biquad instance processes one L/R pair;
	// each side needs independent state, but they share the same response.
	z1L, z2L float64
	z1R, z2R float64

	sampleRate float64
}

// NewBiquad builds a filter and publishes its initial coefficients.
func NewBiquad(kind FilterKind, sampleRate, freq, gainDB, q float64, enabled bool) *Biquad {
	b := &Biquad{
		Kind:       kind,
		Freq:       NewParam(freq),
		GainDB:     NewParam(gainDB),
		Q:          NewParam(q),
		sampleRate: sampleRate,
	}
	b.Enabled.Store(enabled)
	b.Update()
	return b
}

// Update recomputes coefficients from the current Freq/GainDB/Q cells and
// publishes them atomically. Called from the control plane on every parameter
// change — never from the audio thread (it involves transcendental math and
// an allocation).
//
// Formulas are the classic RBJ Audio-EQ-Cookbook. With
// ω = 2π·f0/fs and α = sin(ω)/(2Q):
//
//	Peaking (A = 10^(dB/40) — /40 because gain is split between numerator
//	and denominator, giving symmetric boost/cut):
//	  b0=1+αA  b1=-2cosω  b2=1-αA   a0=1+α/A  a1=-2cosω  a2=1-α/A
//	LPF:  b0=b2=(1-cosω)/2  b1=1-cosω   a0=1+α  a1=-2cosω  a2=1-α
//	HPF:  b0=b2=(1+cosω)/2  b1=-(1+cosω) (same a's)
func (b *Biquad) Update() {
	freq := b.Freq.Get()
	// Clamp to a safe range: ω must stay in (0, π) or the filter goes unstable.
	nyquist := b.sampleRate * 0.499
	if freq < 10 {
		freq = 10
	} else if freq > nyquist {
		freq = nyquist
	}
	q := b.Q.Get()
	if q < 0.1 {
		q = 0.1
	}

	omega := 2 * math.Pi * freq / b.sampleRate
	sinW, cosW := math.Sincos(omega)
	alpha := sinW / (2 * q)

	var b0, b1, b2, a0, a1, a2 float64
	switch b.Kind {
	case FilterPeaking:
		a := math.Pow(10, b.GainDB.Get()/40)
		b0 = 1 + alpha*a
		b1 = -2 * cosW
		b2 = 1 - alpha*a
		a0 = 1 + alpha/a
		a1 = -2 * cosW
		a2 = 1 - alpha/a
	case FilterLowPass:
		b0 = (1 - cosW) / 2
		b1 = 1 - cosW
		b2 = (1 - cosW) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW
		a2 = 1 - alpha
	case FilterHighPass:
		b0 = (1 + cosW) / 2
		b1 = -(1 + cosW)
		b2 = (1 + cosW) / 2
		a0 = 1 + alpha
		a1 = -2 * cosW
		a2 = 1 - alpha
	}

	inv := 1 / a0
	b.coeffs.Store(&BiquadCoeffs{
		B0: b0 * inv, B1: b1 * inv, B2: b2 * inv,
		A1: a1 * inv, A2: a2 * inv,
	})
}

// ProcessBlock filters both channels in place. Audio thread only.
func (b *Biquad) ProcessBlock(l, r []float32) {
	if !b.Enabled.Load() {
		return
	}
	c := b.coeffs.Load()
	z1, z2 := b.z1L, b.z2L
	for i, x := range l {
		xf := float64(x)
		y := c.B0*xf + z1
		z1 = c.B1*xf - c.A1*y + z2
		z2 = c.B2*xf - c.A2*y
		l[i] = float32(y)
	}
	b.z1L, b.z2L = z1, z2

	z1, z2 = b.z1R, b.z2R
	for i, x := range r {
		xf := float64(x)
		y := c.B0*xf + z1
		z1 = c.B1*xf - c.A1*y + z2
		z2 = c.B2*xf - c.A2*y
		r[i] = float32(y)
	}
	b.z1R, b.z2R = z1, z2
}
