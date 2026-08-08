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
	Duplex     bool
	MetroBPM   string // persisted tempo rendered into the BPM input
	MetroBeats string // persisted beats-per-bar; selects the matching meter option
}

// Render satisfies element.Component.
func (t TransportBar) Render(b *element.Builder) (x any) {
	b.DivClass("transport").R(
		b.DivClass("brand").T("go-daw"),

		// Next to the brand rather than tucked beside the status dot: on a
		// first launch this is the control that should be easiest to find.
		b.ButtonClass("tour-btn", "id", "tour-btn",
			"title", "Guided tour of the console").T("◈ Tour"),

		b.Wrap(func() {
			attrs := []string{"id", "rec-btn",
				"title", "Record · starts after a one-bar count-in (press again to abort, shift-click to skip the count-in)"}
			if t.Recording {
				attrs = append(attrs, "data-on", "1")
			}
			b.ButtonClass("rec-btn", attrs...).T("● REC")
		}),
		b.SpanClass("rec-time", "id", "rec-time").T(""),

		// Metronome: the browser schedules the beats (app.js) and each one
		// POSTs /api/click; the engine renders the tick into the monitor
		// path only, so the click is never printed to a recording. Rendered
		// as plain controls with ids (not data-param) so the generic
		// parameter dispatcher leaves them alone.
		b.DivClass("metro-box").R(
			b.ButtonClass("metro-btn", "id", "metro-btn",
				"title", "Metronome on/off · tap 3+ times to set tempo").T("◆ CLICK"),
			// The persisted BPM is rendered server-side (not fetched after
			// load) so the input never flashes a default before settling —
			// the same single-source-of-truth render the rest of the page uses.
			b.Input("type", "number", "id", "metro-bpm",
				"min", "30", "max", "300", "step", "1", "value", t.MetroBPM,
				"title", "Beats per minute").R(),
			b.SpanClass("metro-label").T("bpm"),
			// Meter options as data so the persisted selection is one loop —
			// the settings validator whitelists these same values; keep the
			// two lists in step when adding a meter.
			b.Select("id", "metro-beats", "title", "Beats per bar").R(
				b.Wrap(func() {
					meters := []struct{ value, label string }{
						{"2", "2/4"}, {"3", "3/4"}, {"4", "4/4"}, {"6", "6/8"},
					}
					for _, m := range meters {
						attrs := []string{"value", m.value}
						if m.value == t.MetroBeats {
							attrs = append(attrs, "selected", "selected")
						}
						b.Option(attrs...).T(m.label)
					}
				}),
			),
		),

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
