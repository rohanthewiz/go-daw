package source

import (
	"os"
	"testing"
)

// testBank returns a real .sf2 from the repo's soundbanks folder, or skips.
// Skipping (not failing) keeps CI green on machines that haven't downloaded
// banks — the SoundFont path is exercised wherever the assets exist, which
// includes every dev machine that has actually used the feature.
func testBank(t *testing.T) string {
	t.Helper()
	const bank = "../../soundbanks/GeneralUser-GS.sf2"
	if _, err := os.Stat(bank); err != nil {
		t.Skipf("no test bank at %s; skipping SoundFont tests", bank)
	}
	return bank
}

func TestSFSynthNoteLifecycle(t *testing.T) {
	s, err := NewSFSynth(testBank(t), 48000, -6, 0)
	if err != nil {
		t.Fatalf("NewSFSynth: %v", err)
	}
	l, r := make([]float32, 256), make([]float32, 256)

	if s.Read(l, r) != 256 {
		t.Fatal("Read must always fill the full block")
	}
	if blockPeak(l) != 0 {
		t.Fatal("expected silence before any note-on")
	}

	s.NoteOn(60, 0.8)
	// Sample attacks can be soft; a few blocks guarantees audible level.
	var peak float32
	for range 4 {
		s.Read(l, r)
		if p := blockPeak(l); p > peak {
			peak = p
		}
	}
	if peak == 0 {
		t.Fatal("expected audio after note-on")
	}

	// After note-off plus a generous release window the output must decay
	// well below the sounding level (reverb tails mean it may not hit zero).
	s.NoteOff(60)
	for range 400 { // ~2.1 s at 256/48k
		s.Read(l, r)
	}
	s.Read(l, r)
	if tail := blockPeak(l); tail > peak/10 {
		t.Fatalf("expected released note to decay: tail %v vs peak %v", tail, peak)
	}
}

func TestSFSynthProgramChange(t *testing.T) {
	s, err := NewSFSynth(testBank(t), 48000, -6, 0)
	if err != nil {
		t.Fatalf("NewSFSynth: %v", err)
	}
	l, r := make([]float32, 256), make([]float32, 256)

	s.SetProgram(48) // String Ensemble 1
	if s.Program() != 48 {
		t.Fatalf("Program = %d, want 48", s.Program())
	}
	s.NoteOn(60, 0.8)
	for range 4 {
		s.Read(l, r)
	}
	if blockPeak(l) == 0 {
		t.Fatal("expected audio after program change + note-on")
	}
}

// TestSFSynthReadNoAllocs enforces the realtime contract on the meltysynth
// path exactly as TestPolySynthReadNoAllocs does for the additive synth.
func TestSFSynthReadNoAllocs(t *testing.T) {
	s, err := NewSFSynth(testBank(t), 48000, -6, 0)
	if err != nil {
		t.Fatalf("NewSFSynth: %v", err)
	}
	l, r := make([]float32, 256), make([]float32, 256)
	s.NoteOn(60, 0.8)
	s.NoteOn(64, 0.8)
	s.NoteOn(67, 0.8)

	if allocs := testing.AllocsPerRun(100, func() { s.Read(l, r) }); allocs != 0 {
		t.Fatalf("Read allocated %.1f times per call, want 0", allocs)
	}
}

// TestSFSynthBankCache verifies two synths on the same bank share one parsed
// SoundFont — the property that keeps N channels from holding N copies of a
// 100+ MB sample set.
func TestSFSynthBankCache(t *testing.T) {
	bank := testBank(t)
	a, err := NewSFSynth(bank, 48000, -6, 0)
	if err != nil {
		t.Fatalf("NewSFSynth: %v", err)
	}
	b, err := NewSFSynth(bank, 48000, -6, 24)
	if err != nil {
		t.Fatalf("NewSFSynth: %v", err)
	}
	if a.synth.SoundFont != b.synth.SoundFont {
		t.Fatal("expected both synths to share the cached SoundFont instance")
	}
}
