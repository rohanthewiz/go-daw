package ui

import (
	"fmt"
	"strconv"

	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/mixer"
)

// GroupStrip renders one group bus: name, mute, fader, meter.
type GroupStrip struct {
	Grp *mixer.Group
}

// Render satisfies element.Component.
func (gs GroupStrip) Render(b *element.Builder) (x any) {
	g := gs.Grp
	id := strconv.Itoa(g.ID)

	b.DivClass("strip strip-group").R(
		b.DivClass("strip-name").T(g.Name()),
		b.Wrap(func() {
			attrs := []string{"data-target", "grp", "data-id", id, "data-param", "mute"}
			if g.Mute.Load() {
				attrs = append(attrs, "data-on", "1")
			}
			b.ButtonClass("mute-btn", attrs...).T("MUTE")
		}),
		b.DivClass("fader-row").R(
			b.DivClass("fader-wrap").R(
				b.InputClass("fader", "type", "range",
					"data-target", "grp", "data-id", id, "data-param", "gain",
					"min", "-60", "max", "12", "step", "0.1",
					"value", fmt.Sprintf("%.1f", g.GainDB.Get()),
				).R(),
				b.SpanClass("readout fader-readout").F("%.1f", g.GainDB.Get()),
			),
			meterHTML(b, "grp-"+id),
		),
	)
	return
}

// MasterStrip renders the master bus.
type MasterStrip struct {
	M *mixer.Master
}

// Render satisfies element.Component.
func (ms MasterStrip) Render(b *element.Builder) (x any) {
	b.DivClass("strip strip-master").R(
		b.DivClass("strip-name").T("MASTER"),
		b.DivClass("limiter-note").T("limiter on"),
		b.DivClass("fader-row").R(
			b.DivClass("fader-wrap").R(
				b.InputClass("fader", "type", "range",
					"data-target", "master", "data-id", "0", "data-param", "gain",
					"min", "-60", "max", "12", "step", "0.1",
					"value", fmt.Sprintf("%.1f", ms.M.GainDB.Get()),
				).R(),
				b.SpanClass("readout fader-readout").F("%.1f", ms.M.GainDB.Get()),
			),
			meterHTML(b, "master"),
		),
	)
	return
}
