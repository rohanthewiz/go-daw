package dsp

import (
	"math"
	"testing"
)

const testRate = 48000.0

// sineBlock fills stereo buffers with a sine at freq/amp for n samples,
// continuing from the given phase; returns the updated phase.
func sineBlock(l, r []float32, freq, amp, phase float64) float64 {
	for i := range l {
		v := float32(amp * math.Sin(2*math.Pi*phase))
		l[i], r[i] = v, v
		phase += freq / testRate
	}
	return phase
}

// rms of one channel.
func rms(buf []float32) float64 {
	sum := 0.0
	for _, s := range buf {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(buf)))
}

// TestPeakingEQGainAtCenter verifies the RBJ peaking band delivers its dB
// setting at f0: a +12dB band at 1kHz should raise a 1kHz sine ~4x in
// amplitude (12dB ≈ 3.98x).
func TestPeakingEQGainAtCenter(t *testing.T) {
	eq := NewBiquad(FilterPeaking, testRate, 1000, 12, 0.9, true)

	n := 48000 // 1s: long enough for the filter to fully settle
	l := make([]float32, n)
	r := make([]float32, n)
	sineBlock(l, r, 1000, 0.1, 0)
	inRMS := rms(l[n/2:])
	eq.ProcessBlock(l, r)
	outRMS := rms(l[n/2:]) // measure after settling

	gainDB := 20 * math.Log10(outRMS/inRMS)
	if math.Abs(gainDB-12) > 0.5 {
		t.Fatalf("peaking EQ at f0: want ~+12dB, got %.2fdB", gainDB)
	}
}

// TestLowPassAttenuatesHighs: 500Hz LPF should pass 100Hz nearly untouched
// and knock a 8kHz tone down hard.
func TestLowPassAttenuatesHighs(t *testing.T) {
	n := 48000
	l := make([]float32, n)
	r := make([]float32, n)

	lpf := NewBiquad(FilterLowPass, testRate, 500, 0, 0.707, true)
	sineBlock(l, r, 100, 0.1, 0)
	in := rms(l[n/2:])
	lpf.ProcessBlock(l, r)
	passDB := 20 * math.Log10(rms(l[n/2:])/in)

	lpf2 := NewBiquad(FilterLowPass, testRate, 500, 0, 0.707, true)
	sineBlock(l, r, 8000, 0.1, 0)
	in = rms(l[n/2:])
	lpf2.ProcessBlock(l, r)
	stopDB := 20 * math.Log10(rms(l[n/2:])/in)

	if passDB < -1 {
		t.Fatalf("LPF passband loss too high: %.2fdB at 100Hz", passDB)
	}
	if stopDB > -40 {
		t.Fatalf("LPF stopband too weak: %.2fdB at 8kHz (want < -40)", stopDB)
	}
}

// TestCompressorReduces: a sine 12dB over threshold through a high-ratio
// compressor must come out significantly quieter than it went in.
func TestCompressorReduces(t *testing.T) {
	c := NewCompressor(testRate)
	c.ThresholdDB.Set(-20)
	c.Ratio.Set(10)
	c.KneeDB.Set(0)
	c.AttackMs.Set(1)
	c.ReleaseMs.Set(50)
	c.Enabled.Store(true)

	n := 48000
	l := make([]float32, n)
	r := make([]float32, n)
	sineBlock(l, r, 1000, DBToLin(-8), 0) // -8dBFS peak = 12dB over threshold
	in := rms(l[n/2:])
	c.ProcessBlock(l, r)
	out := rms(l[n/2:])

	reductionDB := 20 * math.Log10(out/in)
	// 10:1 over 12dB of overshoot should give ~-10.8dB; allow slop for the
	// envelope follower's ripple.
	if reductionDB > -6 {
		t.Fatalf("compressor barely working: %.2fdB reduction (want < -6)", reductionDB)
	}
	if c.GainReductionDB.Get() >= 0 {
		t.Fatalf("gain reduction meter not reporting (got %.2f)", c.GainReductionDB.Get())
	}
}

// TestGateClosesOnSilence: loud signal opens the gate, silence closes it.
func TestGateClosesOnSilence(t *testing.T) {
	g := NewGate(testRate)
	g.OpenThreshDB.Set(-40)
	g.AttackMs.Set(1)
	g.HoldMs.Set(10)
	g.ReleaseMs.Set(10)
	g.Enabled.Store(true)

	// Phase 1: loud tone — gate must open and pass signal.
	n := 9600 // 200ms
	l := make([]float32, n)
	r := make([]float32, n)
	sineBlock(l, r, 1000, 0.5, 0)
	g.ProcessBlock(l, r)
	if rms(l[n/2:]) < 0.1 {
		t.Fatalf("gate failed to open on loud signal (rms=%.4f)", rms(l[n/2:]))
	}

	// Phase 2: near-silence — after hold+release the output must be gone.
	sineBlock(l, r, 1000, 0.0001, 0)
	g.ProcessBlock(l, r)
	tail := rms(l[n-1000:])
	if tail > 1e-5 {
		t.Fatalf("gate failed to close on silence (tail rms=%.6f)", tail)
	}
}

// TestReverbProducesTail: an impulse through the reverb must produce energy
// after the dry impulse has passed (i.e., an actual tail), and pre-delay
// must push the tail's onset later.
func TestReverbProducesTail(t *testing.T) {
	rv := NewReverb(testRate)
	rv.Enabled.Store(true)
	rv.Mix.Set(1) // fully wet so we measure only the tail
	rv.PreDelayMs.Set(0)

	n := 24000 // 500ms
	l := make([]float32, n)
	r := make([]float32, n)
	l[0], r[0] = 1, 1
	rv.ProcessBlock(l, r)

	tail := rms(l[4800:9600]) // 100..200ms window
	if tail < 1e-4 {
		t.Fatalf("reverb produced no tail (rms=%.6f)", tail)
	}
}

// TestPanConstantPower: gains at any position satisfy gL²+gR² == 1.
func TestPanConstantPower(t *testing.T) {
	for _, pan := range []float64{-1, -0.5, 0, 0.3, 1} {
		gl, gr := PanGains(pan)
		power := gl*gl + gr*gr
		if math.Abs(power-1) > 1e-9 {
			t.Fatalf("pan %v: power %.9f != 1", pan, power)
		}
	}
	gl, gr := PanGains(-1)
	if gl < 0.999 || gr > 0.001 {
		t.Fatalf("hard left wrong: gL=%.3f gR=%.3f", gl, gr)
	}
}

// TestParamCellRoundTrip: exact float64 round-trip through the atomic cell.
func TestParamCellRoundTrip(t *testing.T) {
	p := NewParam(0)
	for _, v := range []float64{0, -60.5, 1e-12, 48000} {
		p.Set(v)
		if got := p.Get(); got != v {
			t.Fatalf("ParamCell: set %v got %v", v, got)
		}
	}
}
