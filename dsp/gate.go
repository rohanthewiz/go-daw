package dsp

import "sync/atomic"

// Gate states. The gate is a small state machine rather than a bare
// threshold comparison because real signals hover around thresholds;
// without hysteresis + hold the gate "chatters" (rapid open/close),
// which sounds far worse than the noise it removes.
//
//	          env > openThresh
//	 closed ───────────────────► attack ──(gain reaches 1)──► open
//	    ▲                                                       │
//	    │                                       env < closeThresh
//	    │                                                       ▼
//	 release ◄──(hold expires)── hold ◄─────────────────────────┘
//	    │  ▲                      │
//	    │  └──(env rises again: back to attack/open)
//	    └──(gain reaches 0: closed)
type gateState int

const (
	gateClosed gateState = iota
	gateAttack
	gateOpen
	gateHold
	gateRelease
)

// Gate is a noise gate with hysteresis, attack, hold, and release.
type Gate struct {
	Enabled       atomic.Bool
	OpenThreshDB  *ParamCell // level that opens the gate
	HysteresisDB  *ParamCell // close threshold = open - hysteresis (prevents chatter)
	AttackMs      *ParamCell // gain ramp 0 -> 1
	HoldMs        *ParamCell // minimum open time after signal drops
	ReleaseMs     *ParamCell // gain ramp 1 -> 0

	state gateState
	gain  float64 // currently applied gain, ramped linearly
	holdN int     // samples remaining in hold state
	env   float64 // fast peak envelope for detection

	sampleRate float64
}

// NewGate returns a gate with sensible speech/instrument defaults, disabled.
func NewGate(sampleRate float64) *Gate {
	g := &Gate{
		OpenThreshDB: NewParam(-50),
		HysteresisDB: NewParam(6),
		AttackMs:     NewParam(1),
		HoldMs:       NewParam(100),
		ReleaseMs:    NewParam(120),
		sampleRate:   sampleRate,
	}
	return g
}

// ProcessBlock gates both channels in place using a stereo-linked detector.
// Audio thread only.
func (g *Gate) ProcessBlock(l, r []float32) {
	if !g.Enabled.Load() {
		return
	}

	openLin := DBToLin(g.OpenThreshDB.Get())
	closeLin := DBToLin(g.OpenThreshDB.Get() - g.HysteresisDB.Get())

	// Per-sample linear gain increments derived from the ms params.
	// Linear ramps (vs exponential) are simpler and click-free at these rates.
	attackStep := msToStep(g.AttackMs.Get(), g.sampleRate)
	releaseStep := msToStep(g.ReleaseMs.Get(), g.sampleRate)
	holdSamples := int(g.HoldMs.Get() * 0.001 * g.sampleRate)

	// Fixed fast envelope: ~0.5ms attack / ~30ms release. The detector must
	// be faster than the gate's own attack or transients get clipped off.
	const envAtk = 0.9
	const envRel = 0.9993

	for i := range l {
		// Stereo-linked detection: gate both sides identically or the
		// stereo image wobbles as each side opens independently.
		d := abs32(l[i])
		if ar := abs32(r[i]); ar > d {
			d = ar
		}
		df := float64(d)
		if df > g.env {
			g.env = df + envAtk*(g.env-df)
		} else {
			g.env = df + envRel*(g.env-df)
		}

		switch g.state {
		case gateClosed:
			if g.env > openLin {
				g.state = gateAttack
			}
		case gateAttack:
			g.gain += attackStep
			if g.gain >= 1 {
				g.gain = 1
				g.state = gateOpen
			}
		case gateOpen:
			if g.env < closeLin {
				g.holdN = holdSamples
				g.state = gateHold
			}
		case gateHold:
			if g.env > openLin {
				g.state = gateOpen // signal came back before hold expired
			} else if g.holdN--; g.holdN <= 0 {
				g.state = gateRelease
			}
		case gateRelease:
			if g.env > openLin {
				g.state = gateAttack // reopen mid-release
			} else {
				g.gain -= releaseStep
				if g.gain <= 0 {
					g.gain = 0
					g.state = gateClosed
				}
			}
		}

		gf := float32(g.gain)
		l[i] *= gf
		r[i] *= gf
	}
}

// msToStep converts a ramp time in ms to a per-sample linear gain increment.
func msToStep(ms, sampleRate float64) float64 {
	samples := ms * 0.001 * sampleRate
	if samples < 1 {
		samples = 1
	}
	return 1 / samples
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
