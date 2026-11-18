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
	// brand-new chain and Store it; the audio thread only ever sees a fully
	// formed chain. The abandoned chain is garbage-collected once the
	// callback moves on — no locks, no torn state. Structural edits CAS the
	// pointer (see editModules) so two concurrent requests can't lose one
	// another's change.
	modules atomic.Pointer[insertChain]

	src atomic.Pointer[source.Source]

	Meter Meter

	// faults counts blocks in which the strip produced NaN/±Inf audio and
	// had to be silenced and reset (see recoverFault). Written by the audio
	// thread, read by the meter loop; monotonic, so a reader that sees it
	// grow knows a new fault happened since its last look.
	faults atomic.Uint64

	// Audio-thread-only state.
	l, r     []float32 // per-channel scratch
	lastGain float64   // previous block's gain multiplier, for ramping
	lastGL   float64   // previous block's pan gains
	lastGR   float64
	// muteGain is the output multiplier the mute ramp ended the last block
	// on: 1 = audible, 0 = fully muted. See ProcessBlock.
	muteGain float64
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
		muteGain: 1,
	}
	name := "Ch " + itoa(id)
	c.name.Store(&name)
	c.modules.Store(&insertChain{})
	return c
}

// insertChain is one immutable generation of a strip's module inserts plus
// that generation's crash record.
//
// failed is a bitmask indexed by position in mods: bit i set means module i
// panicked inside Process and is bypassed from then on. It lives on the
// chain, not the channel, so it resets for free whenever the chain is
// replaced — removing or re-adding the culprit gives it a clean slate, and a
// bit can never land on the wrong module after an edit shifts indexes.
// Positions past 63 are not tracked; a panic there is still contained for
// the block, just retried next block. (A strip with 64 inserts is not a
// realistic console.)
type insertChain struct {
	mods   []module.AudioModule
	failed atomic.Uint64
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
func (c *Channel) Modules() []module.AudioModule { return c.modules.Load().mods }

// editModules applies a copy-on-write edit with a compare-and-swap retry.
// A plain load-modify-store would let two overlapping requests (two tabs, a
// double click) each copy the same old chain, and the second Store would
// silently discard the first one's module. With CAS the loser re-reads the
// winner's chain and re-applies its edit on top. edit must not mutate old.
// Control plane only; the audio thread never writes this pointer.
func (c *Channel) editModules(edit func(old []module.AudioModule) ([]module.AudioModule, error)) error {
	for {
		cur := c.modules.Load()
		next, err := edit(cur.mods)
		if err != nil {
			return err
		}
		if c.modules.CompareAndSwap(cur, &insertChain{mods: next}) {
			return nil
		}
	}
}

// AddModule appends a module copy-on-write (control plane).
func (c *Channel) AddModule(m module.AudioModule) {
	_ = c.editModules(func(old []module.AudioModule) ([]module.AudioModule, error) {
		next := make([]module.AudioModule, len(old), len(old)+1)
		copy(next, old)
		return append(next, m), nil
	})
}

// SetModules replaces the whole insert chain (control plane, e.g. scene recall).
func (c *Channel) SetModules(chain []module.AudioModule) {
	c.modules.Store(&insertChain{mods: chain})
}

// RemoveModule drops the module at idx copy-on-write (control plane).
func (c *Channel) RemoveModule(idx int) error {
	return c.editModules(func(old []module.AudioModule) ([]module.AudioModule, error) {
		if idx < 0 || idx >= len(old) {
			return nil, serr.New("module index out of range")
		}
		next := make([]module.AudioModule, 0, len(old)-1)
		next = append(next, old[:idx]...)
		return append(next, old[idx+1:]...), nil
	})
}

// FailedModules reports which inserts have been bypassed after panicking,
// as chain indexes. Control plane (UI badge, logs).
func (c *Channel) FailedModules() []int {
	ch := c.modules.Load()
	mask := ch.failed.Load()
	var out []int
	for i := range min(len(ch.mods), 64) {
		if mask&(1<<i) != 0 {
			out = append(out, i)
		}
	}
	return out
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

	// Mute is ramped, not cut. A hard cut mid-waveform is a step
	// discontinuity — an audible click on every mute press, the same zipper
	// problem the gain and pan ramps below exist to avoid. So the block in
	// which mute engages still runs the full strip and fades its output
	// 1→0 (tail of ProcessBlock); only once the ramp has reached 0 does the
	// strip take this fast path. Unmuting runs the strip again and fades
	// 0→1. The source was already read above, so files and songs keep
	// their place while muted.
	//
	//	mute pressed   block k: DSP + ramp 1→0   block k+1…: fast path (silent)
	//	unmute         block m: DSP + ramp 0→1   block m+1…: normal
	muted := c.Mute.Load()
	if muted && c.muteGain == 0 {
		// Fully muted: clear the meter but skip all DSP work.
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

	chain := c.modules.Load()
	for i, m := range chain.mods {
		if i < 64 && chain.failed.Load()&(1<<i) != 0 {
			continue // bypassed after an earlier panic
		}
		if !processInsert(m, l, r) {
			// The module died mid-block, so l/r may be half-processed.
			// Silence this block; the fault scan below would not catch
			// finite garbage. Bypass it for the rest of this chain's life.
			for j := range l {
				l[j], r[j] = 0, 0
			}
			if i < 64 {
				chain.failed.Or(1 << i)
			}
			c.faults.Add(1)
		}
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

	// Mute ramp (see the fast path near the top). Steady unmuted state is
	// muteGain == 1 with target 1, which skips the loop entirely.
	muteTarget := 1.0
	if muted {
		muteTarget = 0
	}
	if muteTarget != c.muteGain {
		mStep := (muteTarget - c.muteGain) / float64(n)
		mg := c.muteGain
		for i := range l {
			mg += mStep
			l[i] *= float32(mg)
			r[i] *= float32(mg)
		}
		c.muteGain = muteTarget
	}

	// Fault containment: a non-finite sample would latch into every
	// recursive processor downstream and, via the master limiter, silence
	// the whole console (see dsp.NonFinite). One scan per block is cheap
	// next to the strip's filters.
	if dsp.NonFinite(l, r) {
		c.recoverFault(l, r)
	}

	c.Meter.StoreBlock(l, r)
	return l, r
}

// processInsert runs one module and reports whether it returned normally.
//
// Inserts include third-party .so plugins, and a Go panic on the audio
// callback is not a glitch: it unwinds into cgo and takes down the whole
// process, with any in-progress recording unfinalized. Containing it here
// costs one open-coded defer per module per block (a few ns) and turns a
// crash into a bypassed insert plus a UI badge.
//
// The recover path is the only part that may allocate (the runtime boxes
// the panic value); it runs at most once per module per chain generation,
// since the caller bypasses the module afterwards.
func processInsert(m module.AudioModule, l, r []float32) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	m.Process(l, r)
	return true
}

// recoverFault silences this block and resets every stateful processor on
// the strip so the next block starts from clean state. Audio thread only;
// every Reset is allocation-free.
//
// Module inserts cannot be reset (AudioModule has no such method), so a
// plugin that keeps emitting NaN will fault on every block — the strip stays
// silent and the counter keeps climbing, which is exactly the signal the UI
// needs to point at it. The rest of the mix is unaffected either way.
func (c *Channel) recoverFault(l, r []float32) {
	for i := range l {
		l[i], r[i] = 0, 0
	}
	c.Gate.Reset()
	c.HPF.Reset()
	c.LPF.Reset()
	for _, eq := range c.EQ {
		eq.Reset()
	}
	c.Comp.Reset()
	c.Reverb.Reset()
	// Ramp memories can only be non-finite if a cell held NaN for a block;
	// restart them from silence so the next ramp is well-defined.
	if !dsp.Finite(c.lastGain) {
		c.lastGain = 0
	}
	if !dsp.Finite(c.lastGL) || !dsp.Finite(c.lastGR) {
		c.lastGL, c.lastGR = 0, 0
	}
	c.faults.Add(1)
}

// Faults returns how many blocks this strip has had to silence because its
// output went non-finite. Safe from any goroutine.
func (c *Channel) Faults() uint64 { return c.faults.Load() }

// ApplyParam routes a dotted parameter name ("eq1.freq", "comp.ratio",
// "gate.enabled", ...) to the right cell. Keeping the entire address space in
// one switch gives a single place to see every automatable parameter.
// Control plane only.
func (c *Channel) ApplyParam(name string, value float64) error {
	// clampF cannot catch NaN (every comparison with NaN is false), so a
	// non-finite value is rejected outright rather than stored. JSON cannot
	// encode NaN, but callers other than the HTTP API (tests, future
	// automation) can.
	if !dsp.Finite(value) {
		return serr.New("parameter value must be a finite number", "param", name)
	}
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
