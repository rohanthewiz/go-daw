package ui

import (
	"fmt"
	"strconv"

	"github.com/rohanthewiz/element"
)

// Fader travel, shared by channels, groups and the master. The scale marks
// below are positioned from these bounds, so the three numbers stay in step.
const (
	faderMinDB = -60.0
	faderMaxDB = 12.0
)

// faderMarks are the dB legends printed alongside the fader slot, top down.
// Spacing thins out toward the bottom exactly as on a console: the working
// range around unity gets the resolution, the last 20 dB of travel does not.
var faderMarks = []float64{12, 5, 0, -5, -10, -20, -40, -60}

// faderDeck renders the bottom of a strip — the LED gain readout, the fader in
// its slot with a dB scale, and the level meter:
//
//	┌──────── fader-row ────────┐
//	│ ┌ wrap ┐  ┌scale┐  ┌meter┐│
//	│ │ +0.0 │  │  12 │  │▓▓▓▓▓││   LED readout sits above the cap, as on the
//	│ │  ▓▓  │  │   5 │  │▓▓▓▓▓││   Ui24R strip; the scale column is shared
//	│ │  ██  │  │   0 │  │▓▓▓▓▓││   between the fader and the meter so both
//	│ │  ▓▓  │  │  -5 │  │░░░░░││   read against the same numbers.
//	│ └──────┘  └─────┘  └─────┘│
//	└───────────────────────────┘
//
// The .fader-row / .fader-wrap / .readout class names are load-bearing: the
// tour spotlights .fader-row, and app.js finds the readout by walking up to
// .fader-wrap.
func faderDeck(b *element.Builder, target, id string, gainDB float64, meterKey string) (x any) {
	b.DivClass("fader-row").R(
		b.DivClass("fader-wrap").R(
			b.SpanClass("readout fader-readout").T(readoutText(gainDB, "gain")),
			b.InputClass("fader", "type", "range",
				"data-target", target, "data-id", id, "data-param", "gain",
				"min", fmtF(faderMinDB), "max", fmtF(faderMaxDB), "step", "0.1",
				"value", fmt.Sprintf("%.1f", gainDB),
				"style", fillStyle(gainDB, faderMinDB, faderMaxDB),
			).R(),
		),
		faderScale(b),
		meterHTML(b, meterKey),
	)
	return
}

// faderScale draws the dB legend column. Each mark is absolutely positioned by
// its share of the fader's travel, so the print always lines up with the slot
// whatever height the strip ends up at.
func faderScale(b *element.Builder) (x any) {
	b.DivClass("fader-scale").R(
		b.Wrap(func() {
			for _, db := range faderMarks {
				pct := (db - faderMinDB) / (faderMaxDB - faderMinDB) * 100
				label := strconv.FormatFloat(db, 'f', -1, 64)
				if db > 0 {
					label = "+" + label
				}
				b.SpanClass("fader-mark", "style", fmt.Sprintf("bottom:%.2f%%", pct)).T(label)
			}
		}),
	)
	return
}

// meterHTML renders the vertical meter track for a bus key ("ch-3", "grp-1",
// "master"): an RMS fill bar plus a peak tick, both driven by SSE. The LED
// segmentation is a mask in the stylesheet rather than markup, so the paint
// path stays a single style.height write per frame.
func meterHTML(b *element.Builder, key string) (x any) {
	b.DivClass("meter", "data-meter", key).R(
		b.DivClass("meter-rms").R(),
		b.DivClass("meter-peak").R(),
	)
	return
}
