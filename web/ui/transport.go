package ui

import (
	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/store"
)

// TransportBar renders the top bar: record control, elapsed time, scene
// memory (save/recall/delete), and a connection indicator fed by SSE.
type TransportBar struct {
	Recording bool
	Scenes    []store.SceneInfo
	Duplex    bool
}

// Render satisfies element.Component.
func (t TransportBar) Render(b *element.Builder) (x any) {
	b.DivClass("transport").R(
		b.DivClass("brand").T("go-daw"),

		b.Wrap(func() {
			attrs := []string{"id", "rec-btn"}
			if t.Recording {
				attrs = append(attrs, "data-on", "1")
			}
			b.ButtonClass("rec-btn", attrs...).T("● REC")
		}),
		b.SpanClass("rec-time", "id", "rec-time").T(""),

		b.DivClass("scene-box").R(
			b.Input("type", "text", "id", "scene-name", "placeholder", "scene name").R(),
			b.Button("id", "scene-save").T("Save"),
			b.Select("id", "scene-list").R(
				b.Wrap(func() {
					b.Option("value", "").T("scenes…")
					for _, s := range t.Scenes {
						b.Option("value", s.Name).T(s.Name)
					}
				}),
			),
			b.Button("id", "scene-recall").T("Recall"),
			b.ButtonClass("danger", "id", "scene-delete").T("Delete"),
		),

		b.Wrap(func() {
			if !t.Duplex {
				b.SpanClass("badge warn").T("playback-only")
			}
		}),
		b.SpanClass("conn-dot", "id", "conn-dot", "title", "meter stream").T("●"),
	)
	return
}
