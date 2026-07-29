// Flanger — example EXTERNAL audio module plugin for go-daw.
//
// Build (must use the exact same Go toolchain and go.mod as the go-daw
// binary, which is why this source lives inside the go-daw module):
//
//	go build -buildmode=plugin -o plugins/flanger.so ./plugins/src/flanger
//
// The loader contract is simply: package main + an exported
//
//	func NewModule() module.AudioModule
//
// Everything else (parameter UI, chain insertion, scene persistence) comes
// for free from the AudioModule interface.
package main

import (
	"math"

	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
)

// Flanger mixes the signal with a copy delayed by a few milliseconds, with
// the delay time swept by an LFO. The moving comb-filter notches produce the
// classic "jet engine" sweep. Feedback deepens the notches.
type Flanger struct {
	rate     *dsp.ParamCell // LFO speed in Hz
	depth    *dsp.ParamCell // 0..1 → how far the delay sweeps
	feedback *dsp.ParamCell // 0..0.9
	mix      *dsp.ParamCell

	bufL, bufR []float32
	w          int
	phase      float64
	sampleRate float64
}

// main is never called: -buildmode=plugin ignores it, and it exists only so
// a plain `go build ./...` of the whole repo doesn't fail on this package.
func main() {}

// NewModule is the symbol the go-daw plugin loader looks up.
func NewModule() module.AudioModule {
	return &Flanger{
		rate:     dsp.NewParam(0.3),
		depth:    dsp.NewParam(0.7),
		feedback: dsp.NewParam(0.4),
		mix:      dsp.NewParam(0.5),
	}
}

func (f *Flanger) Name() string { return "flanger" }

const maxFlangeMs = 12.0

func (f *Flanger) Init(sampleRate float64, maxBlock int) error {
	f.sampleRate = sampleRate
	n := int(maxFlangeMs*0.001*sampleRate) + 2
	f.bufL = make([]float32, n)
	f.bufR = make([]float32, n)
	return nil
}

func (f *Flanger) Process(l, r []float32) {
	inc := f.rate.Get() / f.sampleRate
	depth := f.depth.Get()
	fb := float32(f.feedback.Get())
	mix := float32(f.mix.Get())

	// Sweep between ~1ms and maxFlangeMs·depth. The fractional delay is
	// linearly interpolated — without interpolation the sweep "zipper-steps"
	// between integer sample delays and sounds gritty.
	minD := 0.001 * f.sampleRate
	maxD := maxFlangeMs * 0.001 * f.sampleRate * depth
	if maxD < minD+1 {
		maxD = minD + 1
	}

	for i := range l {
		lfo := 0.5 + 0.5*math.Sin(2*math.Pi*f.phase)
		delay := minD + (maxD-minD)*lfo
		idx := float64(f.w) - delay
		for idx < 0 {
			idx += float64(len(f.bufL))
		}
		i0 := int(idx)
		frac := float32(idx - float64(i0))
		i1 := i0 + 1
		if i1 >= len(f.bufL) {
			i1 = 0
		}

		dl := f.bufL[i0]*(1-frac) + f.bufL[i1]*frac
		dr := f.bufR[i0]*(1-frac) + f.bufR[i1]*frac

		f.bufL[f.w] = l[i] + dl*fb
		f.bufR[f.w] = r[i] + dr*fb

		l[i] = l[i]*(1-mix) + dl*mix
		r[i] = r[i]*(1-mix) + dr*mix

		f.w++
		if f.w >= len(f.bufL) {
			f.w = 0
		}
		f.phase += inc
		if f.phase >= 1 {
			f.phase -= 1
		}
	}
}

func (f *Flanger) Params() []module.ParamSpec {
	return []module.ParamSpec{
		{ID: "rate", Name: "Rate", Unit: "Hz", Min: 0.05, Max: 5, Default: 0.3, Scale: module.ScaleLog},
		{ID: "depth", Name: "Depth", Min: 0, Max: 1, Default: 0.7},
		{ID: "feedback", Name: "Feedback", Min: 0, Max: 0.9, Default: 0.4},
		{ID: "mix", Name: "Mix", Min: 0, Max: 1, Default: 0.5},
	}
}

func (f *Flanger) SetParam(id string, v float64) {
	switch id {
	case "rate":
		f.rate.Set(v)
	case "depth":
		f.depth.Set(v)
	case "feedback":
		f.feedback.Set(v)
	case "mix":
		f.mix.Set(v)
	}
}

func (f *Flanger) GetParam(id string) float64 {
	switch id {
	case "rate":
		return f.rate.Get()
	case "depth":
		return f.depth.Get()
	case "feedback":
		return f.feedback.Get()
	case "mix":
		return f.mix.Get()
	}
	return 0
}
