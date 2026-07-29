package dsp

import (
	"math"
	"sync/atomic"
)

// Compressor is a feed-forward dynamics processor with a peak envelope
// follower, quadratic soft knee, and makeup gain. Setting Ratio >= LimiterRatio
// treats the ratio as infinite, turning it into a limiter.
//
// Signal path (per sample, stereo-linked):
//
//	d = max(|l|,|r|) ─► envelope follower ─► dB ─► gain computer ─► 10^(dB/20)
//	                                                      │
//	     l,r  ───────────────────────────────────────────►×──► out
//
// Feed-forward (detector taps the input, not the output) is chosen because
// it is unconditionally stable and its attack behavior is easier to reason
// about than feed-back designs.
type Compressor struct {
	Enabled     atomic.Bool
	ThresholdDB *ParamCell
	Ratio       *ParamCell // 1..20; >= LimiterRatio means infinity:1
	KneeDB      *ParamCell // width of the soft knee, centered on threshold
	AttackMs    *ParamCell
	ReleaseMs   *ParamCell
	MakeupDB    *ParamCell

	// GainReductionDB is published for the UI meter (negative = reducing).
	GainReductionDB *ParamCell

	env float64 // linear peak envelope (audio-thread state)

	// Cached smoothing coefficients. exp() is too costly per sample, so we
	// recompute only when the ms params actually changed (cheap compares).
	aCoef, rCoef   float64
	lastA, lastR   float64
	sampleRate     float64
}

// LimiterRatio and above is treated as an infinite ratio (hard limiting).
const LimiterRatio = 20.0

// NewCompressor returns a gentle-defaults compressor, disabled.
func NewCompressor(sampleRate float64) *Compressor {
	c := &Compressor{
		ThresholdDB:     NewParam(-18),
		Ratio:           NewParam(3),
		KneeDB:          NewParam(6),
		AttackMs:        NewParam(10),
		ReleaseMs:       NewParam(120),
		MakeupDB:        NewParam(0),
		GainReductionDB: NewParam(0),
		sampleRate:      sampleRate,
	}
	c.refreshCoefs(10, 120)
	return c
}

// NewLimiter returns a brickwall-style safety limiter (used on the master
// bus): fast attack, infinite ratio, ceiling just under 0dBFS so a hot mix
// clips gracefully instead of wrapping into digital hash.
func NewLimiter(sampleRate float64) *Compressor {
	c := NewCompressor(sampleRate)
	c.ThresholdDB.Set(-0.3)
	c.Ratio.Set(LimiterRatio)
	c.KneeDB.Set(0)
	c.AttackMs.Set(0.5)
	c.ReleaseMs.Set(60)
	c.Enabled.Store(true)
	return c
}

// refreshCoefs derives the one-pole smoothing coefficients:
// coef = exp(-1 / (ms * fs/1000)) — the standard time-constant mapping where
// the envelope covers ~63% of a step change in the given time.
func (c *Compressor) refreshCoefs(attackMs, releaseMs float64) {
	if attackMs < 0.01 {
		attackMs = 0.01
	}
	if releaseMs < 1 {
		releaseMs = 1
	}
	c.aCoef = math.Exp(-1 / (attackMs * 0.001 * c.sampleRate))
	c.rCoef = math.Exp(-1 / (releaseMs * 0.001 * c.sampleRate))
	c.lastA, c.lastR = attackMs, releaseMs
}

// ProcessBlock compresses both channels in place. Audio thread only.
func (c *Compressor) ProcessBlock(l, r []float32) {
	if !c.Enabled.Load() {
		c.GainReductionDB.Set(0)
		return
	}

	if a, rel := c.AttackMs.Get(), c.ReleaseMs.Get(); a != c.lastA || rel != c.lastR {
		c.refreshCoefs(a, rel)
	}

	threshold := c.ThresholdDB.Get()
	ratio := c.Ratio.Get()
	knee := c.KneeDB.Get()
	makeup := c.MakeupDB.Get()

	// Slope of the gain computer above threshold: (1/ratio - 1).
	// ratio=4 -> -0.75 (4:1); ratio>=LimiterRatio -> -1 (flat ceiling).
	slope := 1/ratio - 1
	if ratio >= LimiterRatio {
		slope = -1
	}

	maxGR := 0.0 // most negative gain reduction this block, for the UI meter

	for i := range l {
		// Stereo-linked peak detection keeps the image stable: both sides
		// always get the identical gain.
		d := abs32(l[i])
		if ar := abs32(r[i]); ar > d {
			d = ar
		}
		df := float64(d)

		// Branching envelope follower: fast coefficient while the signal is
		// rising (attack), slow while falling (release).
		if df > c.env {
			c.env = df + c.aCoef*(c.env-df)
		} else {
			c.env = df + c.rCoef*(c.env-df)
		}

		// Gain computation in the log domain, where compression curves are
		// straight lines and the soft knee is a simple parabola.
		levelDB := LinToDB(c.env)
		over := levelDB - threshold

		var grDB float64
		switch {
		case knee > 0 && 2*math.Abs(over) <= knee:
			// Inside the knee: quadratic interpolation between "no
			// compression" and the full slope, giving a smooth transition.
			t := over + knee/2
			grDB = slope * t * t / (2 * knee)
		case over > 0:
			grDB = slope * over
		default:
			grDB = 0
		}

		if grDB < maxGR {
			maxGR = grDB
		}

		g := float32(math.Pow(10, (grDB+makeup)/20))
		l[i] *= g
		r[i] *= g
	}

	c.GainReductionDB.Set(maxGR)
}
