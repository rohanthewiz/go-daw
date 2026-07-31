package ui

import (
	"strconv"

	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/tutorial"
)

// TutorialPanel is the guided-lesson strip docked under the piano. The server
// renders only the shell (lesson picker + empty progress strip) because all
// tutorial behavior is interaction state: app.js fetches the step data from
// /api/lessons, paints amber guide rings on the piano keys, checks played
// notes from every input route (mouse, computer keyboard, MIDI), and drives
// the Listen demo through the same note API as live play.
//
//	┌ TUTORIAL  [lesson ▾] [Start] [Listen] [Stop] [✓ Count-in]  3/14 · 1 missed   desc ┐
//	│ (C)(C)(G)(G)(A)(A)(G)(F)(F)(E)(E)(D)(D)(C)   ← step chips, scrolls              │
//	└───────────────────────────────────────────────────────────────────────────────────┘
type TutorialPanel struct {
	CountIn bool // render the count-in toggle checked (persisted preference)
}

func (tp TutorialPanel) Render(b *element.Builder) (x any) {
	b.DivClass("tutorial", "id", "tutorial").R(
		b.DivClass("tut-head").R(
			b.SpanClass("tut-title").T("Tutorial"),
			b.Select("id", "tut-lesson", "title", "Choose a lesson").R(
				b.Wrap(func() {
					// Options carry the catalog index; the client resolves the
					// index against the JSON it fetched, so the two views can
					// never disagree about which lesson is which.
					for i, l := range tutorial.Lessons() {
						b.Option("value", strconv.Itoa(i)).T(l.Name)
					}
				}),
			),
			b.Button("id", "tut-start", "title", "Begin the lesson").T("Start"),
			b.Button("id", "tut-demo", "title", "Hear the lesson played on the synth").T("Listen"),
			b.Button("id", "tut-stop", "title", "Stop the lesson or demo").T("Stop"),
			// The persisted state renders server-side (checked attr) like the
			// metronome inputs, so the toggle never flashes a default; app.js
			// reads it at Start/Listen time and persists changes.
			b.LabelClass("tut-countin", "title",
				"Play four pacing clicks at the metronome tempo before a lesson or demo begins").R(
				b.Wrap(func() {
					attrs := []string{"type", "checkbox", "id", "tut-countin"}
					if tp.CountIn {
						attrs = append(attrs, "checked", "checked")
					}
					b.Input(attrs...).R()
				}),
				b.T("Count-in"),
			),
			b.SpanClass("tut-progress", "id", "tut-progress").R(),
			b.SpanClass("tut-msg", "id", "tut-msg").R(),
		),
		b.DivClass("tut-strip", "id", "tut-strip").R(),
	)
	return
}
