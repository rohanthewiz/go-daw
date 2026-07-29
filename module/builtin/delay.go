package builtin

import (
	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
)

// Delay is a stereo feedback delay (echo). Independent circular buffers per
// side share one set of parameters:
//
//	out = dry·(1-mix) + delayed·mix
//	buf[w] = dry + delayed·feedback
type Delay struct {
	timeMs   *dsp.ParamCell // echo spacing
	feedback *dsp.ParamCell // 0..0.95 — capped below 1 or the loop self-oscillates into runaway
	mix      *dsp.ParamCell

	bufL, bufR []float32
	w          int
	sampleRate float64
}

const maxDelaySeconds = 2

// NewDelay is the registry factory.
func NewDelay() module.AudioModule {
	return &Delay{
		timeMs:   dsp.NewParam(350),
		feedback: dsp.NewParam(0.35),
		mix:      dsp.NewParam(0.3),
	}
}

func (d *Delay) Name() string { return "delay" }

func (d *Delay) Init(sampleRate float64, maxBlock int) error {
	d.sampleRate = sampleRate
	n := int(maxDelaySeconds * sampleRate)
	d.bufL = make([]float32, n)
	d.bufR = make([]float32, n)
	return nil
}

func (d *Delay) Process(l, r []float32) {
	delaySamples := int(d.timeMs.Get() * 0.001 * d.sampleRate)
	if delaySamples < 1 {
		delaySamples = 1
	} else if delaySamples >= len(d.bufL) {
		delaySamples = len(d.bufL) - 1
	}
	fb := float32(d.feedback.Get())
	mix := float32(d.mix.Get())

	for i := range l {
		readIdx := d.w - delaySamples
		if readIdx < 0 {
			readIdx += len(d.bufL)
		}
		dl, dr := d.bufL[readIdx], d.bufR[readIdx]

		d.bufL[d.w] = l[i] + dl*fb
		d.bufR[d.w] = r[i] + dr*fb

		l[i] = l[i]*(1-mix) + dl*mix
		r[i] = r[i]*(1-mix) + dr*mix

		d.w++
		if d.w >= len(d.bufL) {
			d.w = 0
		}
	}
}

func (d *Delay) Params() []module.ParamSpec {
	return []module.ParamSpec{
		{ID: "time", Name: "Time", Unit: "ms", Min: 10, Max: 2000, Default: 350, Scale: module.ScaleLog},
		{ID: "feedback", Name: "Feedback", Min: 0, Max: 0.95, Default: 0.35},
		{ID: "mix", Name: "Mix", Min: 0, Max: 1, Default: 0.3},
	}
}

func (d *Delay) SetParam(id string, v float64) {
	switch id {
	case "time":
		d.timeMs.Set(v)
	case "feedback":
		d.feedback.Set(v)
	case "mix":
		d.mix.Set(v)
	}
}

func (d *Delay) GetParam(id string) float64 {
	switch id {
	case "time":
		return d.timeMs.Get()
	case "feedback":
		return d.feedback.Get()
	case "mix":
		return d.mix.Get()
	}
	return 0
}
