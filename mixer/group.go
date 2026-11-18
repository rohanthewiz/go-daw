package mixer

import (
	"sync/atomic"

	"github.com/rohanthewiz/go-daw/dsp"
)

// Group is a summing bus. Channels assigned to it are mixed together, then
// the group applies its own gain/mute before feeding the master. Groups give
// one fader over related channels (all drums, all vocals) — the reason
// they're worth a whole bus rather than linked channel faders is that
// group-level processing hears the *combined* signal.
type Group struct {
	ID     int
	name   atomic.Pointer[string]
	GainDB *dsp.ParamCell
	Mute   atomic.Bool
	Meter  Meter

	// Accumulators owned by the audio thread; channels sum into these,
	// then the engine calls ProcessBlock.
	L, R     []float32
	lastGain float64
	// muteGain is where the mute ramp ended last block (1 audible, 0
	// muted); same click-free scheme as Channel.ProcessBlock.
	muteGain float64
}

// NewGroup builds a bus with unity gain.
func NewGroup(id, maxBlock int) *Group {
	g := &Group{
		ID:       id,
		GainDB:   dsp.NewParam(0),
		L:        make([]float32, maxBlock),
		R:        make([]float32, maxBlock),
		lastGain: 1,
		muteGain: 1,
	}
	name := "Grp " + itoa(id)
	g.name.Store(&name)
	return g
}

// Name returns the display name.
func (g *Group) Name() string { return *g.name.Load() }

// SetName replaces the display name (control plane).
func (g *Group) SetName(n string) { g.name.Store(&n) }

// Clear zeroes the accumulators at the top of a block. Audio thread.
func (g *Group) Clear(n int) {
	for i := 0; i < n; i++ {
		g.L[i], g.R[i] = 0, 0
	}
}

// ProcessBlock applies gain/mute to the summed bus and meters it.
// Audio thread.
func (g *Group) ProcessBlock(n int) {
	l, r := g.L[:n], g.R[:n]

	// Fully muted (ramp finished): silence without the gain pass. The block
	// in which mute engages falls through and fades out below instead of
	// cutting, which would click.
	muted := g.Mute.Load()
	if muted && g.muteGain == 0 {
		for i := range l {
			l[i], r[i] = 0, 0
		}
		g.Meter.StoreBlock(l, r)
		return
	}

	// The mute ramp folds into the gain ramp: both are linear per-sample
	// multipliers over the same block, so one loop applies their product.
	muteTarget := 1.0
	if muted {
		muteTarget = 0
	}
	target := dsp.DBToLin(g.GainDB.Get())
	step := (target - g.lastGain) / float64(n)
	mStep := (muteTarget - g.muteGain) / float64(n)
	gain, mg := g.lastGain, g.muteGain
	for i := range l {
		gain += step
		mg += mStep
		l[i] *= float32(gain * mg)
		r[i] *= float32(gain * mg)
	}
	g.lastGain = target
	g.muteGain = muteTarget

	g.Meter.StoreBlock(l, r)
}
