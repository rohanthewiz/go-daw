package store

import (
	"path/filepath"
	"testing"
)

// TestLessonProgressRoundTrip exercises the pass-recording rules (completions
// accumulate, best_misses is a lifetime minimum, last_misses is this run) and
// persistence across a store reopen.
func TestLessonProgressRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Three runs of one lesson: sloppy, better, worse again. Best must stick
	// at the minimum while last tracks the most recent run.
	for _, misses := range []int{5, 1, 3} {
		if err := st.RecordLessonPass("2 · C Major Scale", misses); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := st.RecordLessonPass("6 · First Chords", 0); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: migration probe must detect the existing table, rows persist.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	list, err := st2.ListProgress()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 lessons, got %d: %+v", len(list), list)
	}

	byName := map[string]LessonProgress{}
	for _, p := range list {
		byName[p.Lesson] = p
	}

	scale := byName["2 · C Major Scale"]
	if scale.Completions != 3 || scale.BestMisses != 1 || scale.LastMisses != 3 {
		t.Fatalf("scale record wrong: %+v", scale)
	}
	if scale.UpdatedAt.IsZero() {
		t.Fatal("scale updated_at not set")
	}

	chords := byName["6 · First Chords"]
	if chords.Completions != 1 || chords.BestMisses != 0 || chords.LastMisses != 0 {
		t.Fatalf("chords record wrong: %+v", chords)
	}
}
