// Package ui renders the mixer console to HTML with the element builder.
// Every interactive control carries data-* attributes (channel, parameter,
// scaling) so a single delegated JavaScript listener can drive the whole
// surface — no per-control wiring anywhere.
package ui

import (
	"fmt"
	"math"
	"strconv"

	"github.com/rohanthewiz/element"
)

// fmtF trims a float for attribute/readout use.
func fmtF(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// logPos maps a value onto a 0..1000 slider position logarithmically.
// Log mapping matters for frequency-like ranges: on a linear 20..20000 slider
// the entire bass register would occupy the first few pixels.
func logPos(v, min, max float64) int {
	if v < min {
		v = min
	} else if v > max {
		v = max
	}
	return int(1000 * math.Log(v/min) / math.Log(max/min))
}

// slider renders one labeled horizontal control row:
//
//	<div class="ctl"><label>..</label><input type=range ...><span class="readout">..</span></div>
//
// target is "ch", "grp", or "master"; scale "log" switches the slider to the
// 0..1000 log mapping (JS mirrors the same math via data-min/data-max).
func slider(b *element.Builder, target string, id int, param, label string,
	min, max, step, value float64, scale string) (x any) {

	attrs := []string{
		"type", "range",
		"data-target", target,
		"data-id", strconv.Itoa(id),
		"data-param", param,
	}
	if scale == "log" {
		attrs = append(attrs,
			"min", "0", "max", "1000", "step", "1",
			"value", strconv.Itoa(logPos(value, min, max)),
			"data-log", "1",
			"data-min", fmtF(min), "data-max", fmtF(max),
		)
	} else {
		attrs = append(attrs,
			"min", fmtF(min), "max", fmtF(max), "step", fmtF(step),
			"value", fmtF(value),
		)
	}

	b.DivClass("ctl").R(
		b.Label().T(label),
		b.Input(attrs...).R(),
		b.SpanClass("readout").T(readoutText(value, param)),
	)
	return
}

// toggle renders an on/off checkbox bound to a *.enabled style parameter.
func toggle(b *element.Builder, target string, id int, param, label string, on bool) (x any) {
	attrs := []string{
		"type", "checkbox",
		"data-target", target,
		"data-id", strconv.Itoa(id),
		"data-param", param,
	}
	if on {
		attrs = append(attrs, "checked", "checked")
	}
	b.LabelClass("toggle").R(
		b.Input(attrs...).R(),
		b.Span().T(label),
	)
	return
}

// readoutText formats a value for the small numeric display next to a slider.
func readoutText(v float64, param string) string {
	return fmt.Sprintf("%.1f", v)
}
