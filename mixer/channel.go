// Package mixer implements the console topology: N input channels with full
// DSP strips, G group buses, and one master bus.
package mixer

import (
	"strings"
	"sync/atomic"

	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/serr"
)

// Channel is one input strip. Processing order:
//
//	source → input gain → gate → HPF → LPF → EQ1 → EQ2 → EQ3
//	       → comp/limiter → [module inserts] → reverb → pan → meter
//
// The order encodes standard console practice: the gate acts on the raw
// signal (before EQ can shift energy across its threshold), corrective
// filters precede dynamics (so the compressor doesn't chase rumble the HPF
// removes anyway), and the reverb sits last so its tail isn't re-compressed
// into a pumping mess.
type Channel struct {
	ID   int
	name atomic.Pointer[string]

	// Control cells — written by HTTP handlers, read by the audio thread.
	GainDB *dsp.ParamCell // input gain, -60..+12 dB
	Pan    *dsp.ParamCell // -1..+1
	Mute   atomic.Bool
	// GroupIdx: 0 = route direct to master, 1..G = group bus number.
	GroupIdx atomic.Int32

	Gate   *dsp.Gate
	HPF    *dsp.Biquad
	LPF    *dsp.Biquad
	EQ     [3]*dsp.Biquad
	Comp   *dsp.Compressor
	Reverb *dsp.Reverb

	// modules is the insert chain, swapped copy-on-write: handlers build a
	// brand-new slice and Store it; the audio thread only ever sees a fully
	// formed chain. The abandoned slice is garbage-collected once the
	// callback moves on — no locks, no torn state.
	modules atomic.Pointer[[]module.AudioModule]

	src atomic.Pointer[source.Source]

	Meter Meter

	// Audio-thread-only state.
	l, r     []float32 // per-channel scratch
	lastGain float64   // previous block's gain multiplier, for ramping
	lastGL   float64   // previous block's pan gains
	lastGR   float64
}

// NewChannel builds a strip with unity gain, center pan, all processors at
// sensible defaults (dynamics/gate/reverb disabled until the user engages them).
func NewChannel(id int, sampleRate float64, maxBlock int) *Channel {
	c := &Channel{
		ID:     id,
		GainDB: dsp.NewParam(0),
		Pan:    dsp.NewParam(0),
		Gate:   dsp.NewGate(sampleRate),
		HPF:    dsp.NewBiquad(dsp.FilterHighPass, sampleRate, 20, 0, 0.707, false),
		LPF:    dsp.NewBiquad(dsp.FilterLowPass, sampleRate, 20000, 0, 0.707, false),
		EQ: [3]*dsp.Biquad{
			dsp.NewBiquad(dsp.FilterPeaking, sampleRate, 100, 0, 0.9, true),
			dsp.NewBiquad(dsp.FilterPeaking, sampleRate, 1000, 0, 0.9, true),
			dsp.NewBiquad(dsp.FilterPeaking, sampleRate, 8000, 0, 0.9, true),
		},
		Comp:     dsp.NewCompressor(sampleRate),
		Reverb:   dsp.NewReverb(sampleRate),
		l:        make([]float32, maxBlock),
		r:        make([]float32, maxBlock),
		lastGain: 1, lastGL: 0.7071, lastGR: 0.7071,
	}
	name := "Ch " + itoa(id)
	c.name.Store(&name)
	empty := []module.AudioModule{}
	c.modules.Store(&empty)
	return c
}

// Name returns the display name.
func (c *Channel) Name() string { return *c.name.Load() }

// SetName replaces the display name (control plane).
func (c *Channel) SetName(n string) { c.name.Store(&n) }

// Source returns the current source, or nil.
func (c *Channel) Source() source.Source {
	if p := c.src.Load(); p != nil {
		return *p
	}
	return nil
}

// SetSource swaps the input source (control plane).
func (c *Channel) SetSource(s source.Source) { c.src.Store(&s) }

// Modules returns the current insert chain (read-only snapshot).
func (c *Channel) Modules() []module.AudioModule { return *c.modules.Load() }

// AddModule appends a module copy-on-write (control plane).
func (c *Channel) AddModule(m module.AudioModule) {
	old := *c.modules.Load()
	next := make([]module.AudioModule, len(old), len(old)+1)
	copy(next, old)
	next = append(next, m)
	c.modules.Store(&next)
}

// SetModules replaces the whole insert chain (control plane, e.g. scene recall).
func (c *Channel) SetModules(chain []module.AudioModule) {
	c.modules.Store(&chain)
}

// RemoveModule drops the module at idx copy-on-write (control plane).
func (c *Channel) RemoveModule(idx int) error {
	old := *c.modules.Load()
	if idx < 0 || idx >= len(old) {
		return serr.New("module index out of range")
	}
	next := make([]module.AudioModule, 0, len(old)-1)
	next = append(next, old[:idx]...)
	next = append(next, old[idx+1:]...)
	c.modules.Store(&next)
	return nil
}

// ProcessBlock renders n frames and returns the channel's stereo output.
// Audio thread only.
func (c *Channel) ProcessBlock(n int) (l, r []float32) {
	l, r = c.l[:n], c.r[:n]

	// Pull from the source; zero-fill any shortfall so a finished file or
	// missing source yields silence.
	got := 0
	if s := c.Source(); s != nil {
		got = s.Read(l, r)
	}
	for i := got; i < n; i++ {
		l[i], r[i] = 0, 0
	}

	if c.Mute.Load() {
		// Muted channels still clear their meter but skip all DSP work.
		for i := range l {
			l[i], r[i] = 0, 0
		}
		c.Meter.StoreBlock(l, r)
		return l, r
	}

	// Input gain, ramped linearly across the block. Jumping gain in one step
	// creates a waveform discontinuity heard as a "zipper" click; a per-block
	// ramp (~5ms) is fast enough to feel instant and slow enough to be silent.
	targetGain := dsp.DBToLin(c.GainDB.Get())
	step := (targetGain - c.lastGain) / float64(n)
	g := c.lastGain
	for i := range l {
		g += step
		l[i] *= float32(g)
		r[i] *= float32(g)
	}
	c.lastGain = targetGain

	c.Gate.ProcessBlock(l, r)
	c.HPF.ProcessBlock(l, r)
	c.LPF.ProcessBlock(l, r)
	for _, eq := range c.EQ {
		eq.ProcessBlock(l, r)
	}
	c.Comp.ProcessBlock(l, r)

	for _, m := range *c.modules.Load() {
		m.Process(l, r)
	}

	c.Reverb.ProcessBlock(l, r)

	// Constant-power pan, also ramped per block for the same zipper reason.
	tGL, tGR := dsp.PanGains(c.Pan.Get())
	stepL := (tGL - c.lastGL) / float64(n)
	stepR := (tGR - c.lastGR) / float64(n)
	gl, gr := c.lastGL, c.lastGR
	for i := range l {
		gl += stepL
		gr += stepR
		l[i] *= float32(gl)
		r[i] *= float32(gr)
	}
	c.lastGL, c.lastGR = tGL, tGR

	c.Meter.StoreBlock(l, r)
	return l, r
}

// ApplyParam routes a dotted parameter name ("eq1.freq", "comp.ratio",
// "gate.enabled", ...) to the right cell. Keeping the entire address space in
// one switch gives a single place to see every automatable parameter.
// Control plane only.
func (c *Channel) ApplyParam(name string, value float64) error {
	onOff := value >= 0.5 // toggles arrive as 0/1

	switch name {
	case "gain":
		c.GainDB.Set(clampF(value, -60, 12))
	case "pan":
		c.Pan.Set(clampF(value, -1, 1))
	case "mute":
		c.Mute.Store(onOff)

	case "gate.enabled":
		c.Gate.Enabled.Store(onOff)
	case "gate.threshold":
		c.Gate.OpenThreshDB.Set(clampF(value, -90, 0))
	case "gate.hysteresis":
		c.Gate.HysteresisDB.Set(clampF(value, 0, 24))
	case "gate.attack":
		c.Gate.AttackMs.Set(clampF(value, 0.1, 100))
	case "gate.hold":
		c.Gate.HoldMs.Set(clampF(value, 0, 1000))
	case "gate.release":
		c.Gate.ReleaseMs.Set(clampF(value, 1, 2000))

	case "hpf.enabled":
		c.HPF.Enabled.Store(onOff)
	case "hpf.freq":
		c.HPF.Freq.Set(clampF(value, 20, 2000))
		c.HPF.Update()
	case "lpf.enabled":
		c.LPF.Enabled.Store(onOff)
	case "lpf.freq":
		c.LPF.Freq.Set(clampF(value, 200, 20000))
		c.LPF.Update()

	case "comp.enabled":
		c.Comp.Enabled.Store(onOff)
	case "comp.threshold":
		c.Comp.ThresholdDB.Set(clampF(value, -60, 0))
	case "comp.ratio":
		c.Comp.Ratio.Set(clampF(value, 1, dsp.LimiterRatio))
	case "comp.knee":
		c.Comp.KneeDB.Set(clampF(value, 0, 24))
	case "comp.attack":
		c.Comp.AttackMs.Set(clampF(value, 0.1, 200))
	case "comp.release":
		c.Comp.ReleaseMs.Set(clampF(value, 5, 2000))
	case "comp.makeup":
		c.Comp.MakeupDB.Set(clampF(value, 0, 24))

	case "reverb.enabled":
		c.Reverb.Enabled.Store(onOff)
	case "reverb.predelay":
		c.Reverb.PreDelayMs.Set(clampF(value, 0, 250))
	case "reverb.decay":
		c.Reverb.Decay.Set(clampF(value, 0, 1))
	case "reverb.damp":
		c.Reverb.Damp.Set(clampF(value, 0, 1))
	case "reverb.mix":
		c.Reverb.Mix.Set(clampF(value, 0, 1))

	default:
		// EQ bands share a pattern: eq<n>.{freq|gain|q|enabled}
		if strings.HasPrefix(name, "eq") && len(name) > 4 {
			band := int(name[2] - '1')
			if band >= 0 && band < 3 {
				eq := c.EQ[band]
				switch name[4:] {
				case "enabled":
					eq.Enabled.Store(onOff)
					return nil
				case "freq":
					eq.Freq.Set(clampF(value, 20, 20000))
				case "gain":
					eq.GainDB.Set(clampF(value, -18, 18))
				case "q":
					eq.Q.Set(clampF(value, 0.1, 10))
				default:
					return serr.New("unknown EQ parameter", "param", name)
				}
				eq.Update()
				return nil
			}
		}
		return serr.New("unknown channel parameter", "param", name)
	}
	return nil
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// itoa avoids importing strconv for one tiny use in the hot path of nothing.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
