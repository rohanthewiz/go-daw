package ui

import (
	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/mixer"
	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/go-daw/store"
)

// PageData carries everything the mixer page needs at render time.
type PageData struct {
	Console   *mixer.Console
	Scenes    []store.SceneInfo
	Recording bool
	Duplex    bool
}

// MixerPage renders the complete console HTML document. The page is fully
// server-rendered from live mixer state — after structural changes (add
// module, change source, recall scene) the client simply reloads, which
// keeps the client JS small and the server the single source of truth.
func MixerPage(d PageData) string {
	b := element.NewBuilder()
	moduleNames := module.Available()

	b.Html().R(
		b.Head().R(
			b.Title().T("go-daw mixer"),
			b.Meta("charset", "utf-8").R(),
			b.Meta("name", "viewport", "content", "width=device-width, initial-scale=1").R(),
			b.Link("rel", "stylesheet", "href", "/styles.css").R(),
		),
		b.Body().R(
			element.RenderComponents(b, TransportBar{
				Recording: d.Recording,
				Scenes:    d.Scenes,
				Duplex:    d.Duplex,
			}),

			b.DivClass("console").R(
				b.DivClass("channels").R(
					b.Wrap(func() {
						for _, ch := range d.Console.Channels {
							element.RenderComponents(b, ChannelStrip{
								Ch:          ch,
								GroupCount:  len(d.Console.Groups),
								ModuleNames: moduleNames,
								Duplex:      d.Duplex,
							})
						}
					}),
				),
				b.DivClass("buses").R(
					b.Wrap(func() {
						for _, g := range d.Console.Groups {
							element.RenderComponents(b, GroupStrip{Grp: g})
						}
					}),
					element.RenderComponents(b, MasterStrip{M: d.Console.Master}),
				),
			),

			element.RenderComponents(b, PianoPanel{Console: d.Console}),

			b.Script("src", "/assets/app.js").R(),
		),
	)
	return b.String()
}
