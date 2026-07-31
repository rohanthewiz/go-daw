package store

import (
	"path/filepath"
	"testing"
)

// TestSettingsRoundTrip exercises the three states a setting moves through —
// absent, set, overwritten — and persistence across a store reopen.
func TestSettingsRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Absent key: no error, found=false — callers use this to fall back to
	// their compiled-in default.
	if _, found, err := st.GetSetting("metro.bpm"); err != nil || found {
		t.Fatalf("absent key: found=%v err=%v", found, err)
	}

	// Set then overwrite: last write must win.
	if err := st.SetSetting("metro.bpm", "96"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := st.SetSetting("metro.bpm", "132"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: migration probe must detect the existing table, value persists.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	v, found, err := st2.GetSetting("metro.bpm")
	if err != nil || !found || v != "132" {
		t.Fatalf("after reopen: value=%q found=%v err=%v", v, found, err)
	}
}
