package ui

import (
	"fmt"
	"strconv"

	"github.com/rohanthewiz/element"
	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/mixer"
)

// PianoPanel is the playable on-screen keyboard docked under the console. It
// renders 25 keys (two octaves plus the top C) as static structure; which
// MIDI notes they map to is client-side state (the octave shift), so keys
// carry a 0-based data-idx and app.js adds the current base note.
//
//	white keys: flex row, equal widths        black keys: absolute overlay
//	┌──┬──┬──┬──┬──┬──┬──┬──┐                     ▄▄  ▄▄     ▄▄  ▄▄  ▄▄
//	│C │D │E │F │G │A │B │C │ ...  + overlay →   │C#││D#│   │F#││G#││A#│
//	└──┴──┴──┴──┴──┴──┴──┴──┘
//
// Blacks are positioned in percentages computed here at render time rather
// than in CSS, so the layout math lives in one place and works for any key
// count without hand-tuned offsets.
type PianoPanel struct {
	Console *mixer.Console
}

const pianoKeyCount = 25 // two octaves + top C

// pianoHints maps key index → computer-keyboard character, the classic
// tracker/DAW layout: home row = white keys, row above = black keys.
var pianoHints = []string{
	"a", "w", "s", "e", "d", "f", "t", "g", "y", "h", "u", "j", // first octave
	"k", "o", "l", "p", ";", // into the second octave
}

// isBlackKey reports whether a semitone offset from C is a black key.
func isBlackKey(semitone int) bool {
	switch semitone % 12 {
	case 1, 3, 6, 8, 10: // C# D# F# G# A#
		return true
	}
	return false
}

func (pp PianoPanel) Render(b *element.Builder) (x any) {
	// Preselect the first channel already running a synth so the piano is
	// playable immediately; other channels are offered but flagged, since
	// choosing one installs a synth on it (handled in app.js).
	selectedID := 0
	for _, ch := range pp.Console.Channels {
		if _, isSynth := ch.Source().(*source.PolySynth); isSynth {
			selectedID = ch.ID
			break
		}
	}

	b.DivClass("piano", "id", "piano").R(
		b.DivClass("piano-head").R(
			b.SpanClass("piano-title").T("Piano"),
			b.Label().T("Ch"),
			b.Select("id", "piano-channel").R(
				b.Wrap(func() {
					for _, ch := range pp.Console.Channels {
						_, isSynth := ch.Source().(*source.PolySynth)
						label := strconv.Itoa(ch.ID) + " · " + ch.Name()
						attrs := []string{"value", strconv.Itoa(ch.ID)}
						if isSynth {
							attrs = append(attrs, "data-synth", "1")
						} else {
							label += " (→ synth)"
						}
						if ch.ID == selectedID {
							attrs = append(attrs, "selected", "selected")
						}
						b.Option(attrs...).T(label)
					}
				}),
			),
			b.Button("id", "piano-oct-down", "title", "Octave down (Z)").T("−"),
			b.SpanClass("piano-oct", "id", "piano-oct").T("C4"),
			b.Button("id", "piano-oct-up", "title", "Octave up (X)").T("+"),
			// Web MIDI shell: device enumeration and permission live entirely
			// in the browser, so the server can only render an empty select
			// plus a status dot. app.js fills the list (and re-fills it on
			// hot-plug), or hides the whole box where Web MIDI is unsupported.
			b.SpanClass("midi-box", "id", "midi-box").R(
				b.SpanClass("midi-dot", "id", "midi-dot", "title", "MIDI input status").T("●"),
				b.Select("id", "midi-in", "title", "MIDI input device").R(
					b.Option("value", "").T("MIDI: none"),
				),
			),
			b.SpanClass("piano-hint").T("Play: A S D F … (W E T Y U for sharps) · Z/X octave · click or drag"),
		),
		b.DivClass("piano-keys", "id", "piano-keys").R(
			b.Wrap(func() {
				// 15 white keys share the width; blacks sit on the seams at
				// ~62% of a white key's width, like the real proportions.
				const whiteCount = 15
				whiteW := 100.0 / whiteCount
				blackW := whiteW * 0.62

				// Pass 1: white keys in normal flex flow.
				for s := range pianoKeyCount {
					if isBlackKey(s) {
						continue
					}
					b.DivClass("piano-key white", "data-idx", strconv.Itoa(s)).R(
						pianoHintSpan(b, s),
					)
				}
				// Pass 2: black keys as an absolute overlay, each centered on
				// the boundary after its preceding white key.
				whites := 0
				for s := range pianoKeyCount {
					if !isBlackKey(s) {
						whites++
						continue
					}
					left := float64(whites)*whiteW - blackW/2
					b.DivClass("piano-key black", "data-idx", strconv.Itoa(s),
						"style", fmt.Sprintf("left:%.3f%%;width:%.3f%%", left, blackW)).R(
						pianoHintSpan(b, s),
					)
				}
			}),
		),
	)
	return
}

func pianoHintSpan(b *element.Builder, idx int) (x any) {
	if idx < len(pianoHints) {
		b.SpanClass("key-hint").T(pianoHints[idx])
	}
	return
}
