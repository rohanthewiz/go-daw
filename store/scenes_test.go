package store

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/rohanthewiz/go-daw/mixer"
)

// TestSceneRoundTrip exercises the full digital-memory path: snapshot a real
// console, save, reopen the store (fresh process simulation), load, apply.
func TestSceneRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	con := mixer.NewConsole(4, 2, 48000, 4096)
	con.Channels[0].GainDB.Set(-7.5)
	con.Channels[0].Pan.Set(0.33)
	con.Channels[1].Mute.Store(true)
	con.Channels[2].GroupIdx.Store(2)
	con.Channels[0].EQ[1].GainDB.Set(6)
	con.Channels[0].EQ[1].Update()
	con.Channels[0].Reverb.PreDelayMs.Set(80)
	con.Groups[1].GainDB.Set(-3)
	con.Master.GainDB.Set(-1.5)

	blob, err := json.Marshal(con.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := st.Save("soundcheck", blob); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Overwrite via upsert must not error or duplicate.
	if err := st.Save("soundcheck", blob); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: schema probe must detect the existing table, data must persist.
	st2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	list, err := st2.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "soundcheck" {
		t.Fatalf("list wrong: %+v", list)
	}

	loaded, err := st2.Load("soundcheck")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var scene mixer.SceneState
	if err := json.Unmarshal(loaded, &scene); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Apply onto a fresh console and verify key values arrived.
	con2 := mixer.NewConsole(4, 2, 48000, 4096)
	if err := con2.ApplyScene(scene); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if g := con2.Channels[0].GainDB.Get(); g != -7.5 {
		t.Fatalf("gain not restored: %v", g)
	}
	if !con2.Channels[1].Mute.Load() {
		t.Fatal("mute not restored")
	}
	if gi := con2.Channels[2].GroupIdx.Load(); gi != 2 {
		t.Fatalf("group not restored: %v", gi)
	}
	if pd := con2.Channels[0].Reverb.PreDelayMs.Get(); pd != 80 {
		t.Fatalf("predelay not restored: %v", pd)
	}
	if mg := con2.Master.GainDB.Get(); mg != -1.5 {
		t.Fatalf("master gain not restored: %v", mg)
	}

	// Delete and confirm gone.
	if err := st2.Delete("soundcheck"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st2.Load("soundcheck"); err == nil {
		t.Fatal("expected load of deleted scene to fail")
	}
}
