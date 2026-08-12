package ui

import "testing"

// TestReadoutText pins the console readout formats. These strings are printed
// by the server on first render and reprinted by fmtParam() in app.js as soon
// as the operator touches the control, so any drift between the two shows up
// as a value that changes the instant a slider is grabbed. If a case here
// changes, the matching case in app.js has to change with it.
func TestReadoutText(t *testing.T) {
	cases := []struct {
		param string
		value float64
		want  string
	}{
		{"pan", 0, "C"},
		{"pan", 0.004, "C"},   // inside the detent
		{"pan", -0.45, "L45"}, // left of centre
		{"pan", 1, "R100"},
		{"eq1.freq", 100, "100"},
		{"eq2.freq", 1000, "1.0k"},
		{"hpf.freq", 12500, "12.5k"},
		{"gain", 0, "+0.0"},     // fader at unity
		{"gain", -6.5, "-6.5"},  // and below it
		{"eq1.gain", 3, "+3.0"}, // EQ boost/cut is signed too
		{"comp.makeup", 6, "+6.0"},
		{"comp.ratio", 4, "4.0:1"},
		{"eq3.q", 0.9, "0.90"},
		{"comp.attack", 12.5, "12.5"},
		{"gate.release", 250, "250"}, // ms drops the decimal once it is long
		{"reverb.mix", 0.35, "35%"},
		{"reverb.decay", 1, "100%"},
		{"gate.threshold", -42, "-42.0"}, // plain dB, no sign forced
		{"osc.level", -18, "-18.0"},
		{"rate", 5.5, "5.5"}, // module params fall through to the default
	}

	for _, c := range cases {
		if got := readoutText(c.value, c.param); got != c.want {
			t.Errorf("readoutText(%v, %q) = %q, want %q", c.value, c.param, got, c.want)
		}
	}
}

// TestFillStyle checks the slider fill fraction, which the stylesheet
// multiplies by a track length — a percentage or an out-of-range value would
// break the gradient rather than merely misplace it.
func TestFillStyle(t *testing.T) {
	cases := []struct {
		pos, min, max float64
		want          string
	}{
		{0, -60, 12, "--f:0.8333"},   // a fader at unity sits 5/6 up its travel
		{-60, -60, 12, "--f:0.0000"}, // fully down
		{12, -60, 12, "--f:1.0000"},  // fully up
		{500, 0, 1000, "--f:0.5000"}, // log sliders arrive pre-mapped
		{-90, -60, 12, "--f:0.0000"}, // out of range clamps rather than escapes
		{99, -60, 12, "--f:1.0000"},
		{5, 5, 5, "--f:0"}, // degenerate range must not divide by zero
	}

	for _, c := range cases {
		if got := fillStyle(c.pos, c.min, c.max); got != c.want {
			t.Errorf("fillStyle(%v, %v, %v) = %q, want %q", c.pos, c.min, c.max, got, c.want)
		}
	}
}
