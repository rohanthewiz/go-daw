package ui

import (
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

	b.DivClass("strip strip-group", "data-kind", "grp").R(
		b.DivClass("strip-head").R(
			b.DivClass("strip-no").T("GRP"),
			b.DivClass("strip-name").T(esc(g.Name())),
		),
		b.Wrap(func() {
			attrs := []string{"data-target", "grp", "data-id", id, "data-param", "mute"}
			if g.Mute.Load() {
				attrs = append(attrs, "data-on", "1")
			}
			b.ButtonClass("mute-btn", attrs...).T("MUTE")
		}),
		faderDeck(b, "grp", id, g.GainDB.Get(), "grp-"+id),
	)
	return
}

// MasterStrip renders the master bus.
type MasterStrip struct {
	M *mixer.Master
}

// Render satisfies element.Component.
func (ms MasterStrip) Render(b *element.Builder) (x any) {
	b.DivClass("strip strip-master", "data-kind", "master").R(
		b.DivClass("strip-head").R(
			b.DivClass("strip-no").T("MAIN"),
			b.DivClass("strip-name").T("MASTER"),
		),
		// The safety limiter is always in circuit, so it reads as a lit status
		// lamp rather than as a control the operator could reach for.
		b.DivClass("limiter-note").T("LIMITER"),
		faderDeck(b, "master", "0", ms.M.GainDB.Get(), "master"),
	)
	return
}
