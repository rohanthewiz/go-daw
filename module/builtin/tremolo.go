// Package builtin holds the compiled-in audio modules. They serve double
// duty: useful effects out of the box, and reference implementations for
// external plugin authors (same interface, same realtime rules).
package builtin

import (
	"math"

	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
)

// Tremolo is a classic LFO amplitude modulator:
//
//	g(t) = 1 - depth·(0.5 + 0.5·sin(2π·rate·t))
//
// The LFO is offset/scaled so gain swings between 1 and (1-depth) —
// modulation only ever attenuates, keeping headroom safe.
type Tremolo struct {
	rate  *dsp.ParamCell // Hz
	depth *dsp.ParamCell // 0..1

	phase      float64
	sampleRate float64
}

// NewTremolo is the registry factory.
func NewTremolo() module.AudioModule {
	return &Tremolo{
		rate:  dsp.NewParam(5),
		depth: dsp.NewParam(0.6),
	}
}

func (t *Tremolo) Name() string { return "tremolo" }

func (t *Tremolo) Init(sampleRate float64, maxBlock int) error {
	t.sampleRate = sampleRate
	return nil
}

func (t *Tremolo) Process(l, r []float32) {
	inc := t.rate.Get() / t.sampleRate
	depth := t.depth.Get()
	for i := range l {
		lfo := 0.5 + 0.5*math.Sin(2*math.Pi*t.phase)
		g := float32(1 - depth*lfo)
		l[i] *= g
		r[i] *= g
		t.phase += inc
		if t.phase >= 1 {
			t.phase -= 1
		}
	}
}

func (t *Tremolo) Params() []module.ParamSpec {
	return []module.ParamSpec{
		{ID: "rate", Name: "Rate", Unit: "Hz", Min: 0.1, Max: 20, Default: 5, Scale: module.ScaleLog},
		{ID: "depth", Name: "Depth", Min: 0, Max: 1, Default: 0.6},
	}
}

func (t *Tremolo) SetParam(id string, v float64) {
	switch id {
	case "rate":
		t.rate.Set(v)
	case "depth":
		t.depth.Set(v)
	}
}

func (t *Tremolo) GetParam(id string) float64 {
	switch id {
	case "rate":
		return t.rate.Get()
	case "depth":
		return t.depth.Get()
	}
	return 0
}
