// Cross Fader — external audio module plugin for go-daw.
//
// Build (must use the exact same Go toolchain and go.mod as the go-daw
// binary, which is why this source lives inside the go-daw module):
//
//	go build -buildmode=plugin -o plugins/cross_fader.so ./plugins/src/cross_fader
//
// Loader contract: package main + an exported
//
//	func NewModule() module.AudioModule
//
// What it does: DJ-style fades — a single track up or down, or a linked
// crossfade *across* several tracks at once.
//
// The multi-track trick: AudioModule instances are per-channel inserts and
// can't see each other's audio, but every instance from one .so shares the
// plugin package's globals. The X-Fade position lives in one package-level
// ParamCell, so moving "position" on ANY channel's cross_fader moves the
// whole crossfade. Each instance then only decides how it *responds* via
// its per-instance Side / Depth / Fade Time params.
//
//	   master position (shared, 0=A .. 1=B)
//	        │
//	  ┌─────┴──────┬─────────────┐
//	  ▼            ▼             ▼
//	ch1 (Side A) ch2 (Side A)  ch3 (Side B)
//	fades OUT    fades OUT     fades IN     as position sweeps 0 → 1
//
// Gains follow an equal-power law (cos/sin of position·π/2) so the summed
// loudness stays constant through the middle of the fade — a linear
// crossfade dips ~3 dB at the midpoint, which is exactly the "hole" DJs
// complain about.
//
// Single-track use: insert on one channel, leave Side at A, sweep position
// 0→1 to fade the track down (or Side B to fade it up). Set Fade Time to,
// say, 3 s and a single position flip becomes a timed auto-fade.
package main

import (
	"math"

	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
)

// position is deliberately package-level: one .so is loaded once per
// process, so this cell is the shared "master crossfader" every instance
// reads. Writes come from the control plane (any channel's UI slider),
// reads from the audio thread — the ParamCell atomic covers both.
var position = dsp.NewParam(0)

// CrossFader is the per-channel view onto the shared crossfade.
type CrossFader struct {
	side  *dsp.ParamCell // 0 = side A (loud at position 0), 1 = side B (loud at 1)
	depth *dsp.ParamCell // how much the fade affects this track; 0 = bypass
	fade  *dsp.ParamCell // seconds for the audio-side position to reach a new target

	// Audio-thread-only state (no atomics needed: Process is the sole
	// reader and writer).
	curPos   float64 // slewed position, chasing the shared target
	prevGain float32 // last block's end gain, start point for this block's ramp
	primed   bool    // first Process snaps state instead of ramping from defaults

	sampleRate float64
}

// main is never called: -buildmode=plugin ignores it; it exists only so a
// plain `go build ./...` of the whole repo doesn't fail on this package.
func main() {}

// NewModule is the symbol the go-daw plugin loader looks up.
func NewModule() module.AudioModule {
	return &CrossFader{
		side:  dsp.NewParam(0),
		depth: dsp.NewParam(1),
		fade:  dsp.NewParam(0.05),
	}
}

func (c *CrossFader) Name() string { return "cross_fader" }

func (c *CrossFader) Init(sampleRate float64, maxBlock int) error {
	c.sampleRate = sampleRate
	return nil
}

// gainAt maps a position to this instance's gain. Equal-power halves:
// side A tracks cos(p·π/2), side B tracks sin(p·π/2); a fractional Side
// setting blends the two laws, and Depth pulls the result toward unity so
// a track can ride the crossfade only partially (e.g. duck 6 dB, not out).
func (c *CrossFader) gainAt(pos, side, depth float64) float32 {
	gA := math.Cos(pos * math.Pi / 2)
	gB := math.Sin(pos * math.Pi / 2)
	g := gA*(1-side) + gB*side
	return float32(1 - depth*(1-g))
}

func (c *CrossFader) Process(l, r []float32) {
	n := len(l)
	if n == 0 {
		return
	}
	target := position.Get()
	side := c.side.Get()
	depth := c.depth.Get()
	fadeSec := c.fade.Get()

	// Slew the audio-side position toward the shared target at block
	// granularity: over `fadeSec` seconds the position travels the full
	// 0..1 range, so a position flip becomes a timed fade. Below ~1 ms we
	// just snap — the per-block gain ramp underneath already de-zippers
	// the jump within the block.
	if fadeSec < 0.001 {
		c.curPos = target
	} else {
		maxStep := float64(n) / c.sampleRate / fadeSec
		if d := target - c.curPos; d > maxStep {
			c.curPos += maxStep
		} else if d < -maxStep {
			c.curPos -= maxStep
		} else {
			c.curPos = target
		}
	}

	gain := c.gainAt(c.curPos, side, depth)

	// First block after insertion: apply the current gain flat instead of
	// ramping from the zero-value state, which would fade in from silence
	// hidden behind prevGain==0.
	if !c.primed {
		c.prevGain = gain
		c.primed = true
	}

	// Per-block linear gain ramp (same idiom as the console's gain/pan
	// ramps): two trig calls per block, then a cheap incremental multiply
	// per sample — smooth enough that even a 10 s fade shows no stepping,
	// since adjacent block endpoints differ by well under 0.001 gain.
	step := (gain - c.prevGain) / float32(n)
	g := c.prevGain
	for i := range n {
		g += step
		l[i] *= g
		r[i] *= g
	}
	c.prevGain = gain
}

func (c *CrossFader) Params() []module.ParamSpec {
	return []module.ParamSpec{
		// Shared across every cross_fader instance — see the package comment.
		{ID: "position", Name: "X-Fade (linked)", Min: 0, Max: 1, Default: 0},
		{ID: "side", Name: "Side A/B", Min: 0, Max: 1, Default: 0},
		{ID: "depth", Name: "Depth", Min: 0, Max: 1, Default: 1},
		{ID: "fade", Name: "Fade Time", Unit: "s", Min: 0, Max: 10, Default: 0.05},
	}
}

func (c *CrossFader) SetParam(id string, v float64) {
	switch id {
	case "position":
		position.Set(v) // shared: moves the crossfade for all instances
	case "side":
		c.side.Set(v)
	case "depth":
		c.depth.Set(v)
	case "fade":
		c.fade.Set(v)
	}
}

func (c *CrossFader) GetParam(id string) float64 {
	switch id {
	case "position":
		return position.Get()
	case "side":
		return c.side.Get()
	case "depth":
		return c.depth.Get()
	case "fade":
		return c.fade.Get()
	}
	return 0
}
