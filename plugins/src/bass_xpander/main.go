// Bass Xpander — external audio module plugin for go-daw.
//
// Build (must use the exact same Go toolchain and go.mod as the go-daw
// binary, which is why this source lives inside the go-daw module):
//
//	go build -buildmode=plugin -o plugins/bass_xpander.so ./plugins/src/bass_xpander
//
// Loader contract: package main + an exported
//
//	func NewModule() module.AudioModule
//
// What it does: fattens the low end two ways at once —
//
//  1. Harmonic excitation: the band below the crossover is driven through a
//     tanh waveshaper. The added odd harmonics sit an octave-and-up above the
//     fundamental, which is what lets small speakers *imply* bass they can't
//     physically reproduce.
//  2. Sub-octave synthesis: a flip-flop divider toggles on every rising
//     zero-crossing of the (mono-summed) low band, producing a square wave at
//     half the fundamental frequency. Low-passed into a near-sine and scaled
//     by the low band's envelope so it breathes with the performance.
//
// Signal flow:
//
//	          ┌────────────┐   ┌──────────────┐
//	 in ──┬──▶│ LPF @xover │──▶│ tanh(drive·x)│───────────── harm·amount ─┐
//	      │   └─────┬──────┘   └──────────────┘                           │
//	      │         │  mono sum                                           ▼
//	      │         └─▶ zero-cross flip-flop ─▶ LPF ─▶ ×envelope ─ sub ─▶(+)─▶ out
//	      │                                                               ▲
//	      └────────────────────────────────── dry ────────────────────────┘
//
// The dry path is untouched (this is an *expander*, not a replacement), so
// at amount=0 and sub=0 the module is bit-transparent minus float rounding.
package main

import (
	"math"

	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
)

// BassXpander holds per-instance DSP state. Each channel insert gets its own
// instance from the factory, so filter/flip-flop state never crosses channels.
type BassXpander struct {
	xover  *dsp.ParamCell // crossover frequency in Hz — everything below is "bass"
	drive  *dsp.ParamCell // waveshaper input gain (1..10)
	amount *dsp.ParamCell // level of generated harmonics mixed in (0..1)
	sub    *dsp.ParamCell // level of the synthesized sub-octave (0..1)

	// Two cascaded one-pole lowpass stages per channel ≈ 12 dB/oct. Cheap,
	// allocation-free, and steep enough to isolate bass; a biquad would be
	// overkill for a crossover whose exact slope is not audible here.
	lp1L, lp2L float32
	lp1R, lp2R float32

	// Sub-octave generator state. The flip-flop halves the frequency of the
	// mono low band; env is a fast-attack/slow-release follower so the sub
	// dies out with the note instead of droning at a fixed level.
	flip    float32 // current divider output: +1 or -1
	prevLow float32 // previous mono low sample, for zero-cross detection
	subLP   float32 // smoothing filter state: square → near-sine
	env     float32 // envelope of the low band

	sampleRate float64
}

// main is never called: -buildmode=plugin ignores it; it exists only so a
// plain `go build ./...` of the whole repo doesn't fail on this package.
func main() {}

// NewModule is the symbol the go-daw plugin loader looks up.
func NewModule() module.AudioModule {
	return &BassXpander{
		xover:  dsp.NewParam(150),
		drive:  dsp.NewParam(4),
		amount: dsp.NewParam(0.5),
		sub:    dsp.NewParam(0.4),
		flip:   1,
	}
}

func (b *BassXpander) Name() string { return "bass_xpander" }

// Init has nothing to allocate — all state is fixed-size struct fields —
// but we still capture the sample rate for per-block coefficient math.
func (b *BassXpander) Init(sampleRate float64, maxBlock int) error {
	b.sampleRate = sampleRate
	return nil
}

func (b *BassXpander) Process(l, r []float32) {
	// Params are read once per block, not per sample: atomics are cheap but
	// not free, and intra-block parameter zipper on a bass effect is inaudible.
	fc := b.xover.Get()
	drive := float32(b.drive.Get())
	amount := float32(b.amount.Get())
	subLvl := float32(b.sub.Get())

	// One-pole coefficient: y += a·(x − y), a = 1 − e^(−2π·fc/fs).
	// Exact-decay form rather than the bilinear approximation so the
	// crossover stays accurate even at low fc/fs ratios.
	a := float32(1 - math.Exp(-2*math.Pi*fc/b.sampleRate))

	// The sub square wave is smoothed at half the crossover — its own
	// fundamental — rounding it toward a sine so it reads as a note, not a buzz.
	aSub := float32(1 - math.Exp(-math.Pi*fc/b.sampleRate))

	// Envelope follower: ~5 ms attack so the sub speaks with the transient,
	// ~80 ms release so it hangs on through the note without pumping.
	atk := float32(1 - math.Exp(-1/(0.005*b.sampleRate)))
	rel := float32(1 - math.Exp(-1/(0.080*b.sampleRate)))

	for i := range l {
		// --- isolate the low band (two cascaded one-poles ≈ 12 dB/oct) ---
		b.lp1L += a * (l[i] - b.lp1L)
		b.lp2L += a * (b.lp1L - b.lp2L)
		b.lp1R += a * (r[i] - b.lp1R)
		b.lp2R += a * (b.lp1R - b.lp2R)
		lowL, lowR := b.lp2L, b.lp2R

		// --- harmonic excitation ---
		// tanh(drive·x)/drive keeps perceived level roughly constant as drive
		// rises, so the drive knob changes *color* more than loudness. We add
		// only the difference (saturated − clean) so amount controls purely
		// the generated harmonics, never doubling the clean low band.
		harmL := float32(math.Tanh(float64(drive*lowL)))/drive - lowL
		harmR := float32(math.Tanh(float64(drive*lowR)))/drive - lowR

		// --- sub-octave synthesis (mono: sub-bass has no useful stereo) ---
		low := 0.5 * (lowL + lowR)

		// Envelope with instant-ish attack, slow release.
		mag := low
		if mag < 0 {
			mag = -mag
		}
		if mag > b.env {
			b.env += atk * (mag - b.env)
		} else {
			b.env += rel * (mag - b.env)
		}

		// Flip on rising zero-crossings only → output period is exactly twice
		// the input period, i.e. one octave down. A tiny hysteresis threshold
		// keeps noise-floor chatter from false-triggering the divider.
		const thresh = 1e-4
		if b.prevLow <= thresh && low > thresh {
			b.flip = -b.flip
		}
		b.prevLow = low

		// Square → near-sine, then scale by the envelope so the sub tracks
		// the source's dynamics instead of gating on/off at full level.
		b.subLP += aSub * (b.flip - b.subLP)
		subOut := b.subLP * b.env

		// --- sum: dry passes through untouched ---
		l[i] += harmL*amount + subOut*subLvl
		r[i] += harmR*amount + subOut*subLvl
	}
}

func (b *BassXpander) Params() []module.ParamSpec {
	return []module.ParamSpec{
		{ID: "xover", Name: "Crossover", Unit: "Hz", Min: 50, Max: 400, Default: 150, Scale: module.ScaleLog},
		{ID: "drive", Name: "Drive", Min: 1, Max: 10, Default: 4},
		{ID: "amount", Name: "Harmonics", Min: 0, Max: 1, Default: 0.5},
		{ID: "sub", Name: "Sub Level", Min: 0, Max: 1, Default: 0.4},
	}
}

func (b *BassXpander) SetParam(id string, v float64) {
	switch id {
	case "xover":
		b.xover.Set(v)
	case "drive":
		b.drive.Set(v)
	case "amount":
		b.amount.Set(v)
	case "sub":
		b.sub.Set(v)
	}
}

func (b *BassXpander) GetParam(id string) float64 {
	switch id {
	case "xover":
		return b.xover.Get()
	case "drive":
		return b.drive.Get()
	case "amount":
		return b.amount.Get()
	case "sub":
		return b.sub.Get()
	}
	return 0
}
