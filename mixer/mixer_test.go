package mixer

import (
	"math"
	"testing"

	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/module"
)

const (
	testRate  = 48000.0
	testBlock = 256
)

// poisonSource emits finite DC until poisoned, then NaN — standing in for a
// misbehaving plugin or a corrupt float WAV. Switching it back to clean
// models the user fixing the cause (removing the plugin, swapping the file).
type poisonSource struct {
	poisoned bool
	level    float32
}

func (p *poisonSource) Read(l, r []float32) int {
	v := p.level
	if p.poisoned {
		v = float32(math.NaN())
	}
	for i := range l {
		l[i], r[i] = v, v
	}
	return len(l)
}

func (p *poisonSource) Name() string { return "poison" }

func allFinite(buf []float32) bool {
	for _, s := range buf {
		if math.IsNaN(float64(s)) || math.IsInf(float64(s), 0) {
			return false
		}
	}
	return true
}

func allZero(buf []float32) bool {
	for _, s := range buf {
		if s != 0 {
			return false
		}
	}
	return true
}

// TestChannelContainsNonFiniteFault drives NaN through a strip with every
// recursive processor engaged and checks the three promises of the fault
// path: the bad block leaves the strip silent (not NaN), the fault is
// counted, and once the source is clean again the strip recovers instead of
// staying latched on NaN state.
func TestChannelContainsNonFiniteFault(t *testing.T) {
	ch := NewChannel(1, testRate, testBlock)
	for _, p := range []string{"hpf.enabled", "lpf.enabled", "comp.enabled", "reverb.enabled"} {
		if err := ch.ApplyParam(p, 1); err != nil {
			t.Fatal(err)
		}
	}
	src := &poisonSource{level: 0.25}
	ch.SetSource(src)

	l, _ := ch.ProcessBlock(testBlock)
	if !allFinite(l) || ch.Faults() != 0 {
		t.Fatalf("clean block: finite=%v faults=%d", allFinite(l), ch.Faults())
	}

	src.poisoned = true
	l, r := ch.ProcessBlock(testBlock)
	if !allZero(l) || !allZero(r) {
		t.Fatal("faulted block should be silenced, got non-zero (NaN?) samples")
	}
	if ch.Faults() != 1 {
		t.Fatalf("faults = %d, want 1", ch.Faults())
	}
	pl, rl, pr, rr := ch.Meter.Levels()
	for _, v := range []float32{pl, rl, pr, rr} {
		if v != v {
			t.Fatal("NaN reached the meter; the SSE frame would fail to encode")
		}
	}

	// Recovery: without the resets, biquad/comp/reverb state would hold NaN
	// and every later block would fault too.
	src.poisoned = false
	before := ch.Faults()
	for range 20 {
		l, _ = ch.ProcessBlock(testBlock)
	}
	if ch.Faults() != before {
		t.Fatalf("strip kept faulting after the source recovered (%d → %d)", before, ch.Faults())
	}
	if !allFinite(l) || allZero(l) {
		t.Fatal("strip did not return to audible, finite output after recovery")
	}
}

// TestApplyParamRejectsNonFinite: clampF lets NaN through (comparisons with
// NaN are false), so ApplyParam must refuse it before it reaches a cell.
func TestApplyParamRejectsNonFinite(t *testing.T) {
	ch := NewChannel(1, testRate, testBlock)
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if err := ch.ApplyParam("gain", v); err == nil {
			t.Fatalf("ApplyParam(gain, %v) accepted a non-finite value", v)
		}
	}
	if g := ch.GainDB.Get(); g != 0 {
		t.Fatalf("gain changed to %v after rejected writes", g)
	}
}

// TestMuteRampsInsteadOfCutting pins the click-free mute: the block in which
// mute engages fades from the running level to ~0, the next block is
// silent, and unmuting fades back up rather than jumping.
func TestMuteRampsInsteadOfCutting(t *testing.T) {
	ch := NewChannel(1, testRate, testBlock)
	ch.SetSource(&poisonSource{level: 0.5}) // clean DC: easy to read ramps off
	ch.ProcessBlock(testBlock)              // settle the gain/pan ramps

	ch.Mute.Store(true)
	l, _ := ch.ProcessBlock(testBlock)
	first, last := l[0], l[testBlock-1]
	if first < 0.3 {
		t.Fatalf("mute cut instead of ramping: first sample %v, want near the unmuted level", first)
	}
	if last > 1e-6 {
		t.Fatalf("mute ramp should end at 0, got %v", last)
	}
	for i := 1; i < testBlock; i++ {
		if l[i] > l[i-1]+1e-7 {
			t.Fatalf("mute ramp not monotonic at %d: %v → %v", i, l[i-1], l[i])
		}
	}

	l, _ = ch.ProcessBlock(testBlock)
	if !allZero(l) {
		t.Fatal("block after the mute ramp should be silent")
	}

	ch.Mute.Store(false)
	l, _ = ch.ProcessBlock(testBlock)
	if l[0] > 0.01 || l[testBlock-1] < 0.3 {
		t.Fatalf("unmute should ramp up from 0: first %v last %v", l[0], l[testBlock-1])
	}
}

// TestGroupMuteRamps: same promise on the bus, where mute folds into the
// gain ramp.
func TestGroupMuteRamps(t *testing.T) {
	g := NewGroup(1, testBlock)
	fill := func() {
		for i := range testBlock {
			g.L[i], g.R[i] = 0.5, 0.5
		}
	}
	fill()
	g.ProcessBlock(testBlock)

	g.Mute.Store(true)
	fill()
	g.ProcessBlock(testBlock)
	if g.L[0] < 0.4 || g.L[testBlock-1] > 1e-6 {
		t.Fatalf("group mute should ramp 0.5→0: first %v last %v", g.L[0], g.L[testBlock-1])
	}
	fill()
	g.ProcessBlock(testBlock)
	if !allZero(g.L[:testBlock]) {
		t.Fatal("group block after the mute ramp should be silent")
	}
}

// TestApplySceneClampsStripValues: a scene is stored (and hand-editable)
// JSON. Values that are destructive rather than merely odd — ratio 0 makes
// the compressor's slope 1/0 — must be clamped on the way in, exactly as a
// live slider move would be.
func TestApplySceneClampsStripValues(t *testing.T) {
	con := NewConsole(1, 1, testRate, testBlock)
	scene := con.Snapshot()
	cs := &scene.Channels[0]
	cs.GainDB = 1000
	cs.Comp.Enabled = true
	cs.Comp.Ratio = 0
	cs.EQ[0].Enabled = true
	cs.EQ[0].Q = 0
	cs.EQ[0].GainDB = -500
	cs.Reverb.Mix = 7
	cs.Source = SourceState{Type: "osc", FreqHz: 440, LevelDB: -6}
	scene.Master.GainDB = 99
	scene.Groups[0].GainDB = -1e9

	if err := con.ApplyScene(scene); err != nil {
		t.Fatal(err)
	}
	ch := con.Channels[0]
	checks := []struct {
		name      string
		got, want float64
	}{
		{"gain", ch.GainDB.Get(), 12},
		{"comp.ratio", ch.Comp.Ratio.Get(), 1},
		{"eq1.q", ch.EQ[0].Q.Get(), 0.1},
		{"eq1.gain", ch.EQ[0].GainDB.Get(), -18},
		{"reverb.mix", ch.Reverb.Mix.Get(), 1},
		{"master gain", con.Master.GainDB.Get(), 12},
		{"group gain", con.Groups[0].GainDB.Get(), -60},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want clamped %v", c.name, c.got, c.want)
		}
	}
	if !ch.Comp.Enabled.Load() || !ch.EQ[0].Enabled.Load() {
		t.Error("toggles were not restored through ApplyParam")
	}

	for range 10 {
		l, _ := ch.ProcessBlock(testBlock)
		if !allFinite(l) {
			t.Fatal("clamped scene still produced non-finite audio")
		}
	}
	if ch.Faults() != 0 {
		t.Fatalf("clamped scene faulted %d blocks", ch.Faults())
	}
}

// TestSceneRoundTrip: Snapshot → ApplyScene on a fresh console reproduces
// the strip, now that recall goes through ApplyParam instead of direct Sets.
func TestSceneRoundTrip(t *testing.T) {
	a := NewConsole(2, 1, testRate, testBlock)
	ch := a.Channels[1]
	for name, v := range map[string]float64{
		"gain": -7, "pan": 0.3, "gate.enabled": 1, "gate.threshold": -40,
		"hpf.enabled": 1, "hpf.freq": 120, "eq2.gain": 4.5, "eq2.q": 2,
		"comp.enabled": 1, "comp.ratio": 6, "reverb.enabled": 1, "reverb.decay": 0.8,
	} {
		if err := ch.ApplyParam(name, v); err != nil {
			t.Fatal(name, err)
		}
	}
	ch.GroupIdx.Store(1)

	want := a.Snapshot()
	b := NewConsole(2, 1, testRate, testBlock)
	if err := b.ApplyScene(want); err != nil {
		t.Fatal(err)
	}
	got := b.Snapshot()
	if got.Channels[1].GainDB != -7 || got.Channels[1].Pan != 0.3 || got.Channels[1].Group != 1 ||
		got.Channels[1].Gate != want.Channels[1].Gate || got.Channels[1].HPF != want.Channels[1].HPF ||
		got.Channels[1].EQ != want.Channels[1].EQ || got.Channels[1].Comp != want.Channels[1].Comp ||
		got.Channels[1].Reverb != want.Channels[1].Reverb {
		t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got.Channels[1], want.Channels[1])
	}
}

// Compile-time check that the test double satisfies the real interface.
var _ source.Source = (*poisonSource)(nil)

// fakeModule is a scriptable insert: it can panic on demand and records
// the last value each parameter was set to.
type fakeModule struct {
	panics bool
	calls  int
	params map[string]float64
}

func (f *fakeModule) Name() string                  { return "fake" }
func (f *fakeModule) Init(float64, int) error       { f.params = map[string]float64{}; return nil }
func (f *fakeModule) GetParam(id string) float64    { return f.params[id] }
func (f *fakeModule) SetParam(id string, v float64) { f.params[id] = v }
func (f *fakeModule) Params() []module.ParamSpec {
	return []module.ParamSpec{{ID: "depth", Min: 0, Max: 1, Default: 0.5}}
}
func (f *fakeModule) Process(l, r []float32) {
	f.calls++
	if f.panics {
		var m map[string]int
		m["boom"]++ // nil-map write: a realistic plugin bug
	}
	for i := range l {
		l[i] *= 0.5
		r[i] *= 0.5
	}
}

// TestPanickingInsertIsBypassed: a plugin panic on the audio thread used to
// take down the whole process. Now the block is silenced, the module is
// bypassed for the rest of this chain's life, the fault is counted, and the
// insert after it keeps running.
func TestPanickingInsertIsBypassed(t *testing.T) {
	ch := NewChannel(1, testRate, testBlock)
	ch.SetSource(&poisonSource{level: 0.5})
	bad, good := &fakeModule{panics: true}, &fakeModule{}
	bad.Init(testRate, testBlock)
	good.Init(testRate, testBlock)
	ch.SetModules([]module.AudioModule{bad, good})

	l, _ := ch.ProcessBlock(testBlock) // must not panic
	if !allZero(l) {
		t.Fatal("block with a panicking insert should be silenced")
	}
	if ch.Faults() != 1 || len(ch.FailedModules()) != 1 || ch.FailedModules()[0] != 0 {
		t.Fatalf("faults=%d failed=%v, want 1 and [0]", ch.Faults(), ch.FailedModules())
	}

	for range 3 {
		l, _ = ch.ProcessBlock(testBlock)
	}
	if bad.calls != 1 {
		t.Fatalf("bypassed module was called %d times, want 1", bad.calls)
	}
	if good.calls != 4 || allZero(l) {
		t.Fatalf("downstream insert stopped running (calls=%d)", good.calls)
	}

	// Replacing the chain gives a clean slate (the mask lives on the chain).
	ch.SetModules([]module.AudioModule{good})
	if len(ch.FailedModules()) != 0 {
		t.Fatal("failed mask should reset with a new chain")
	}
}

// TestConcurrentAddModuleLosesNothing: copy-on-write with a plain Store let
// two overlapping adds each copy the same old chain, dropping one module.
// Run with -race.
func TestConcurrentAddModuleLosesNothing(t *testing.T) {
	ch := NewChannel(1, testRate, testBlock)
	const n = 50
	done := make(chan struct{})
	for range n {
		go func() {
			ch.AddModule(&fakeModule{})
			done <- struct{}{}
		}()
	}
	for range n {
		<-done
	}
	if got := len(ch.Modules()); got != n {
		t.Fatalf("chain has %d modules after %d concurrent adds", got, n)
	}
}

// TestSetModuleParamClampsToSpec: module params are clamped to the module's
// declared range and unknown IDs are refused, so no request can hand a
// plugin a value that hangs or poisons the audio thread.
func TestSetModuleParamClampsToSpec(t *testing.T) {
	m := &fakeModule{}
	m.Init(testRate, testBlock)
	for _, c := range []struct{ in, want float64 }{{1e20, 1}, {-5, 0}, {0.25, 0.25}} {
		if err := SetModuleParam(m, "depth", c.in); err != nil {
			t.Fatal(err)
		}
		if got := m.GetParam("depth"); got != c.want {
			t.Fatalf("SetModuleParam(%v) stored %v, want %v", c.in, got, c.want)
		}
	}
	if err := SetModuleParam(m, "nope", 1); err == nil {
		t.Fatal("unknown parameter accepted")
	}
	if err := SetModuleParam(m, "depth", math.NaN()); err == nil {
		t.Fatal("NaN accepted")
	}
}
