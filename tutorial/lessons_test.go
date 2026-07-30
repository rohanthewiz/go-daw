package tutorial

import "testing"

// TestMidiParsing pins the scientific-pitch → MIDI mapping. C4 = 60 is the
// anchor the whole UI (piano base, lessons) is built around.
func TestMidiParsing(t *testing.T) {
	cases := map[string]int{
		"C4":  60,
		"C#4": 61,
		"A4":  69, // 440 Hz reference
		"C5":  72,
		"E5":  76,
		"F#3": 54,
		"C0":  12,
	}
	for name, want := range cases {
		if got := midi(name); got != want {
			t.Errorf("midi(%q) = %d, want %d", name, got, want)
		}
	}
}

// TestCatalogInvariants checks what the client assumes about every built-in
// lesson: after it shifts the keyboard base down to the C at or below the
// lesson's lowest note, every note of the lesson must land on one of the 25
// visible keys — otherwise guide rings would point off-screen.
func TestCatalogInvariants(t *testing.T) {
	for _, l := range Lessons() {
		if l.Name == "" || l.Desc == "" || len(l.Steps) == 0 {
			t.Fatalf("lesson %q: missing name, desc, or steps", l.Name)
		}

		low := 128
		for _, s := range l.Steps {
			if len(s.Notes) == 0 || s.Label == "" || s.Ms <= 0 {
				t.Fatalf("lesson %q: step %+v missing notes, label, or duration", l.Name, s)
			}
			for _, n := range s.Notes {
				if n < low {
					low = n
				}
			}
		}

		base := low / 12 * 12 // client's fitBase: C at or below the lowest note
		for _, s := range l.Steps {
			for _, n := range s.Notes {
				if n < base || n > base+24 {
					t.Errorf("lesson %q: note %d outside visible window %d..%d",
						l.Name, n, base, base+24)
				}
			}
		}
	}
}
