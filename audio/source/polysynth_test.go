package source

import "testing"

func blockPeak(l []float32) float32 {
	var peak float32
	for _, v := range l {
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

func TestPolySynthNoteLifecycle(t *testing.T) {
	p := NewPolySynth(48000, -12)
	l, r := make([]float32, 256), make([]float32, 256)

	if p.Read(l, r) != 256 {
		t.Fatal("Read must always fill the full block")
	}
	if blockPeak(l) != 0 {
		t.Fatal("expected silence before any note-on")
	}

	p.NoteOn(60, 0.8)
	p.Read(l, r)
	if blockPeak(l) == 0 || blockPeak(r) == 0 {
		t.Fatal("expected audio on both sides after note-on")
	}
	if p.ActiveVoices() != 1 {
		t.Fatalf("ActiveVoices = %d, want 1", p.ActiveVoices())
	}

	// Release is -60 dB in ~150 ms; 100 blocks at 256/48k ≈ 533 ms is ample
	// headroom for the voice to fall below the idle threshold and park.
	p.NoteOff(60)
	for range 100 {
		p.Read(l, r)
	}
	if p.ActiveVoices() != 0 {
		t.Fatalf("voice still active %d after release window", p.ActiveVoices())
	}
}

func TestPolySynthVoiceStealing(t *testing.T) {
	p := NewPolySynth(48000, -12)
	l, r := make([]float32, 256), make([]float32, 256)

	// 24 distinct held notes into a 16-voice pool: the pool must cap, not grow.
	for n := 40; n < 64; n++ {
		p.NoteOn(n, 0.8)
	}
	p.Read(l, r)
	if p.ActiveVoices() != synthVoices {
		t.Fatalf("ActiveVoices = %d, want full pool %d", p.ActiveVoices(), synthVoices)
	}
}

// TestPolySynthReadNoAllocs enforces the project's realtime contract: the
// audio-thread path must never allocate.
func TestPolySynthReadNoAllocs(t *testing.T) {
	p := NewPolySynth(48000, -12)
	l, r := make([]float32, 256), make([]float32, 256)
	p.NoteOn(60, 0.8)
	p.NoteOn(64, 0.8)
	p.NoteOn(67, 0.8)

	if allocs := testing.AllocsPerRun(100, func() { p.Read(l, r) }); allocs != 0 {
		t.Fatalf("Read allocated %.1f times per call, want 0", allocs)
	}
}
