// Package tutorial defines the built-in piano lesson catalog served to the
// web UI. Lessons are pure data: the browser drives key highlighting, input
// checking, and demo playback through the same note API the virtual piano
// already uses, so nothing in this package touches the audio engine or holds
// runtime state.
package tutorial

import (
	"strconv"
	"strings"
)

// Step is one unit of a lesson: the set of MIDI notes that must be held
// together (one note for melodies, several for chords), a short label shown
// in the progress strip, and how long demo playback should sound it.
type Step struct {
	Notes []int  `json:"notes"`
	Label string `json:"label"`
	Ms    int    `json:"ms"`
}

// Lesson groups ordered steps under a name and a one-line instruction.
type Lesson struct {
	Name  string `json:"name"`
	Desc  string `json:"desc"`
	Steps []Step `json:"steps"`
}

// Lessons returns the built-in catalog. The slice is shared, not copied —
// callers must treat it as read-only.
func Lessons() []Lesson { return builtin }

// semitones maps a note letter to its offset within an octave (C = 0).
var semitones = map[byte]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}

// midi converts scientific pitch notation ("C4", "F#3") to a MIDI note
// number (C4 = 60). Lesson data is compiled in, so a malformed name is an
// authoring bug — panicking at package init beats shipping silently wrong
// pitches to every lesson.
func midi(name string) int {
	if len(name) < 2 {
		panic("tutorial: bad note name " + strconv.Quote(name))
	}
	semi, okLetter := semitones[name[0]]
	if !okLetter {
		panic("tutorial: bad note letter in " + strconv.Quote(name))
	}
	rest := name[1:]
	if rest[0] == '#' {
		semi++
		rest = rest[1:]
	}
	oct, err := strconv.Atoi(rest)
	if err != nil {
		panic("tutorial: bad octave in " + strconv.Quote(name))
	}
	return (oct+1)*12 + semi // MIDI octave -1 starts at 0, so C4 = (4+1)*12
}

// note builds a single-pitch melody step. The label drops the octave digit
// ("C4" → "C") because beginners read plain letter names; the strip order
// already conveys direction.
func note(name string, ms int) Step {
	label := strings.TrimRight(strings.TrimSuffix(name, "#"), "0123456789")
	if strings.Contains(name, "#") {
		label += "#"
	}
	return Step{Notes: []int{midi(name)}, Label: label, Ms: ms}
}

// chord builds a hold-together step with an explicit display label ("Am").
func chord(label string, ms int, names ...string) Step {
	ns := make([]int, len(names))
	for i, n := range names {
		ns[i] = midi(n)
	}
	return Step{Notes: ns, Label: label, Ms: ms}
}

// melody expands space-separated pitch names into quarter-note (400ms) demo
// steps; a trailing "-" marks a held note played at double length. This keeps
// tunes readable as one line instead of dozens of note() calls.
func melody(names string) []Step {
	var steps []Step
	for tok := range strings.FieldsSeq(names) {
		ms := 400
		if strings.HasSuffix(tok, "-") {
			ms = 800
			tok = strings.TrimSuffix(tok, "-")
		}
		steps = append(steps, note(tok, ms))
	}
	return steps
}

// builtin is the lesson catalog, ordered easiest first. Every lesson is
// authored to fit within the piano's visible two-octave window once the
// client shifts the base to the lesson's lowest C, so guides are always
// on-screen.
var builtin = []Lesson{
	{
		Name:  "1 · First Steps",
		Desc:  "Middle C and its neighbors — play each highlighted key in turn.",
		Steps: melody("C4 D4 E4 D4 C4-"),
	},
	{
		Name:  "2 · C Major Scale",
		Desc:  "The C major scale, up and back down — all white keys.",
		Steps: melody("C4 D4 E4 F4 G4 A4 B4 C5- B4 A4 G4 F4 E4 D4 C4-"),
	},
	{
		Name:  "3 · Mary Had a Little Lamb",
		Desc:  "Your first melody, built from just four notes.",
		Steps: melody("E4 D4 C4 D4 E4 E4 E4- D4 D4 D4- E4 G4 G4- E4 D4 C4 D4 E4 E4 E4 E4 D4 D4 E4 D4 C4-"),
	},
	{
		Name:  "4 · Twinkle Twinkle",
		Desc:  "Practice the bigger jump from C up to G.",
		Steps: melody("C4 C4 G4 G4 A4 A4 G4- F4 F4 E4 E4 D4 D4 C4-"),
	},
	{
		Name:  "5 · Ode to Joy",
		Desc:  "Beethoven's theme — smooth stepwise motion with one tricky ending.",
		Steps: melody("E4 E4 F4 G4 G4 F4 E4 D4 C4 C4 D4 E4 E4- D4 D4- E4 E4 F4 G4 G4 F4 E4 D4 C4 C4 D4 E4 D4- C4 C4-"),
	},
	{
		Name: "6 · First Chords",
		Desc: "Triads: hold every highlighted key down at the same time.",
		Steps: []Step{
			chord("C", 900, "C4", "E4", "G4"),
			chord("F", 900, "F4", "A4", "C5"),
			chord("G", 900, "G4", "B4", "D5"),
			chord("C", 1200, "C4", "E4", "G4"),
		},
	},
	{
		Name: "7 · Pop Progression",
		Desc: "The I–V–vi–IV progression behind a thousand songs.",
		Steps: []Step{
			chord("C", 900, "C4", "E4", "G4"),
			chord("G", 900, "G4", "B4", "D5"),
			chord("Am", 900, "A4", "C5", "E5"),
			chord("F", 900, "F4", "A4", "C5"),
			chord("C", 1200, "C4", "E4", "G4"),
		},
	},
}
