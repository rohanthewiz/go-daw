package source

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestMidi writes a minimal format-0 SMF to a temp file and returns its
// path: one 0.5-second middle-C note at 120 BPM (480 ticks/quarter, so one
// quarter note = 0.5 s). Hand-assembled bytes keep the test free of any MIDI
// writer dependency and make the expected timeline exact.
func writeTestMidi(t *testing.T) string {
	t.Helper()
	data := []byte{
		// MThd: format 0, 1 track, 480 ticks per quarter note
		'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 0, 0, 1, 0x01, 0xE0,
		// MTrk, payload length 16
		'M', 'T', 'r', 'k', 0, 0, 0, 16,
		0x00, 0xC0, 0x00, // t=0: program change -> 0 (piano)
		0x00, 0x90, 0x3C, 0x64, // t=0: note on C4, vel 100
		0x83, 0x60, 0x80, 0x3C, 0x00, // t=+480 ticks (varlen 0x83 0x60): note off C4
		0x00, 0xFF, 0x2F, 0x00, // end of track
	}
	path := filepath.Join(t.TempDir(), "test.mid")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing test midi: %v", err)
	}
	return path
}

func testMidiPlayer(t *testing.T) *MidiPlayer {
	t.Helper()
	p, err := NewMidiPlayer(testBank(t), writeTestMidi(t), 48000, -6)
	if err != nil {
		t.Fatalf("NewMidiPlayer: %v", err)
	}
	return p
}

func TestMidiPlayerPlayStop(t *testing.T) {
	p := testMidiPlayer(t)
	l, r := make([]float32, 256), make([]float32, 256)

	// Stopped: Read reports zero frames (engine zero-fills) and no state drift.
	if p.Read(l, r) != 0 {
		t.Fatal("stopped player should write no frames")
	}
	if p.PlayState() != "stopped" {
		t.Fatalf("PlayState = %q, want stopped", p.PlayState())
	}
	if got := p.LengthSeconds(); got < 0.45 || got > 0.55 {
		t.Fatalf("LengthSeconds = %v, want ~0.5", got)
	}

	p.Play()
	var peak float32
	for range 8 {
		p.Read(l, r)
		if pk := blockPeak(l); pk > peak {
			peak = pk
		}
	}
	if peak == 0 {
		t.Fatal("expected audio after play (note-on at t=0)")
	}
	if p.PlayState() != "playing" {
		t.Fatalf("PlayState = %q, want playing", p.PlayState())
	}
	if p.PosSeconds() == 0 {
		t.Fatal("position should advance while playing")
	}

	p.Stop()
	// A zero return is the contract's silence signal (the engine zero-fills
	// unwritten frames), so the stale buffer contents are irrelevant here.
	if p.Read(l, r) != 0 {
		t.Fatal("expected zero frames (silence) after stop")
	}
	if p.PlayState() != "stopped" || p.PosSeconds() != 0 {
		t.Fatalf("stop should reset transport: state %q pos %v", p.PlayState(), p.PosSeconds())
	}
}

// TestMidiPlayerPauseResume: pause freezes the song clock (position holds)
// while resume continues from the same point.
func TestMidiPlayerPauseResume(t *testing.T) {
	p := testMidiPlayer(t)
	l, r := make([]float32, 256), make([]float32, 256)

	p.Play()
	for range 8 {
		p.Read(l, r)
	}
	p.Pause()
	p.Read(l, r)
	if p.PlayState() != "paused" {
		t.Fatalf("PlayState = %q, want paused", p.PlayState())
	}
	posAtPause := p.PosSeconds()
	for range 8 {
		p.Read(l, r) // paused blocks render tails but must not advance the clock
	}
	if p.PosSeconds() != posAtPause {
		t.Fatalf("position moved while paused: %v -> %v", posAtPause, p.PosSeconds())
	}

	p.Play() // resume, not restart
	p.Read(l, r)
	if p.PlayState() != "playing" {
		t.Fatalf("PlayState = %q, want playing after resume", p.PlayState())
	}
	if p.PosSeconds() < posAtPause {
		t.Fatal("resume must continue from the pause point, not restart")
	}
}

// TestMidiPlayerAutoStop: a non-looping song flips itself back to stopped
// after the song length plus the tail window, so the UI never shows a
// perpetual "playing" over silence.
func TestMidiPlayerAutoStop(t *testing.T) {
	p := testMidiPlayer(t)
	l, r := make([]float32, 256), make([]float32, 256)

	p.Play()
	// Song 0.5 s + 2 s tail = 2.5 s -> ~470 blocks at 256/48k; 520 adds margin.
	for range 520 {
		p.Read(l, r)
	}
	if p.PlayState() != "stopped" {
		t.Fatalf("PlayState = %q, want auto-stopped after song + tail", p.PlayState())
	}
}

// TestMidiPlayerLoop: with loop latched the note re-strikes every pass, so
// audio is still present far past the song length and the transport still
// reads "playing"; the position readout folds back into the song length.
func TestMidiPlayerLoop(t *testing.T) {
	p := testMidiPlayer(t)
	l, r := make([]float32, 256), make([]float32, 256)

	p.SetLoop(true)
	if !p.Loop() {
		t.Fatal("Loop() should mirror SetLoop(true)")
	}
	p.Play()

	// 3 s in (six passes of the 0.5 s song): scan a window for the re-struck
	// note — a fresh attack lands within any half-second span when looping.
	for range 560 {
		p.Read(l, r)
	}
	var peak float32
	for range 96 {
		p.Read(l, r)
		if pk := blockPeak(l); pk > peak {
			peak = pk
		}
	}
	if peak == 0 {
		t.Fatal("expected looping playback to still produce audio at ~3s")
	}
	if p.PlayState() != "playing" {
		t.Fatalf("PlayState = %q, want playing while looping", p.PlayState())
	}
	if pos := p.PosSeconds(); pos > p.LengthSeconds() {
		t.Fatalf("looped position %v should fold into song length %v", pos, p.LengthSeconds())
	}
}

// TestMidiPlayerReadNoAllocs enforces the realtime contract across the drain
// and sequencer render paths, transport events included — the same guarantee
// TestSFSynthReadNoAllocs gives the live-note path.
func TestMidiPlayerReadNoAllocs(t *testing.T) {
	p := testMidiPlayer(t)
	l, r := make([]float32, 256), make([]float32, 256)
	p.SetLoop(true)
	p.Play()
	p.Read(l, r) // drain the initial play so the loop below measures steady state

	if allocs := testing.AllocsPerRun(100, func() {
		p.Play() // exercises seq.Play/Reset in the drain path
		p.Read(l, r)
	}); allocs != 0 {
		t.Fatalf("Read allocated %.1f times per call, want 0", allocs)
	}
}
