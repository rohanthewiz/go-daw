// Package ui renders the mixer console to HTML with the element builder.
// Every interactive control carries data-* attributes (channel, parameter,
// scaling) so a single delegated JavaScript listener can drive the whole
// surface — no per-control wiring anywhere.
package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

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

// fillStyle renders the --f custom property that paints the filled portion of
// a slider track (the travel behind the cap, as on a console's illuminated
// slot). A native range input gives CSS no handle on "the part below the
// thumb", so the track gradient reads its stop position from --f and the
// client rewrites the property on every input event (see setFill in app.js).
//
// The value is a unitless 0..1 fraction rather than a percentage on purpose:
// the stylesheet multiplies it by a calc() length to inset the fill by half a
// cap, which CSS allows for numbers but not for percentages.
//
// pos/min/max are in slider units — i.e. already 0..1000 for log-scaled
// controls, whose DOM value is the log position and not the parameter value.
func fillStyle(pos, min, max float64) string {
	if max == min {
		return "--f:0"
	}
	f := (pos - min) / (max - min)
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	return fmt.Sprintf("--f:%.4f", f)
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
		pos := logPos(value, min, max)
		attrs = append(attrs,
			"min", "0", "max", "1000", "step", "1",
			"value", strconv.Itoa(pos),
			"data-log", "1",
			"data-min", fmtF(min), "data-max", fmtF(max),
			"style", fillStyle(float64(pos), 0, 1000),
		)
	} else {
		attrs = append(attrs,
			"min", fmtF(min), "max", fmtF(max), "step", fmtF(step),
			"value", fmtF(value),
			"style", fillStyle(value, min, max),
		)
	}

	// A range symmetric about zero (pan, EQ band gain) is a centre-detented
	// control on a console, not a left-to-right level: it gets a tick at the
	// middle and a fill that grows outward from there. Pan additionally runs
	// cyan, as the Ui24R's does.
	cls := "ctl"
	if min < 0 && max > 0 && math.Abs(min+max) < 1e-9 {
		cls += " ctl-center"
	}
	if param == "pan" {
		cls += " ctl-pan"
	}

	b.DivClass(cls).R(
		b.Label().T(label),
		b.Input(attrs...).R(),
		b.SpanClass("readout").T(readoutText(value, param)),
	)
	return
}

// toggle renders an on/off checkbox bound to a *.enabled style parameter.
// The checkbox is styled into a small lit indicator button (see .toggle in the
// stylesheet), so it reads as console hardware while staying a real checkbox
// for keyboard and assistive-tech users.
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
//
// Console readouts are unit-aware — a pan sits at "L45", a filter at "1.2k",
// a trim at "-6.0" — so the format is chosen from the parameter's last dotted
// segment ("eq2.freq" -> "freq"). app.js repeats this switch verbatim in
// fmtParam() so a value typed by the server and the same value re-rendered
// after a drag never disagree; change one and change the other.
func readoutText(v float64, param string) string {
	switch paramKind(param) {
	case "pan":
		// Centre is a detent worth naming, not "0.0".
		n := math.Round(math.Abs(v) * 100)
		switch {
		case n < 1:
			return "C"
		case v < 0:
			return fmt.Sprintf("L%.0f", n)
		default:
			return fmt.Sprintf("R%.0f", n)
		}
	case "freq":
		if v >= 1000 {
			return fmt.Sprintf("%.1fk", v/1000)
		}
		return fmt.Sprintf("%.0f", v)
	case "gain":
		// Boost/cut and fader travel both read better signed.
		return fmt.Sprintf("%+.1f", v)
	case "ratio":
		return fmt.Sprintf("%.1f:1", v)
	case "q":
		return fmt.Sprintf("%.2f", v)
	case "ms":
		if v >= 100 {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%.1f", v)
	case "pct":
		return fmt.Sprintf("%.0f%%", v*100)
	}
	if math.Abs(v) >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// paramKind buckets a parameter name into a readout format. Split out so the
// mapping is one table rather than a chain of suffix tests scattered through
// readoutText.
func paramKind(param string) string {
	name := param
	if i := strings.LastIndex(param, "."); i >= 0 {
		name = param[i+1:]
	}
	switch name {
	case "pan":
		return "pan"
	case "freq":
		return "freq"
	case "gain", "makeup":
		return "gain"
	case "ratio":
		return "ratio"
	case "q":
		return "q"
	case "attack", "release", "hold", "predelay":
		return "ms"
	case "mix", "decay", "damp":
		return "pct"
	}
	return ""
}
