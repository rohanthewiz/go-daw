package source

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/serr"
	"github.com/sinshu/go-meltysynth/meltysynth"
)

// SFSynth renders notes through a SoundFont (.sf2) bank via meltysynth — a
// pure-Go SF2 synthesizer, so no cgo beyond the audio device itself. It is the
// sampled-instrument sibling of PolySynth: same NotePlayer API, same lock-free
// event ring, but the timbre comes from real recorded samples (any of the 128
// General MIDI programs) instead of additive synthesis.
//
//	control plane (HTTP)                    audio thread (Read)
//	NoteOn/NoteOff/SetProgram --mu--> ring[] --atomics--> drain -> meltysynth.Render
//
// meltysynth was verified to honor the realtime contract: its voice pool and
// block buffers are fully allocated in NewSynthesizer, and the NoteOn/NoteOff/
// Render paths neither allocate nor lock, so calling them from the audio
// callback is safe.
const (
	sfRingSize = 128 // power of two so index wrap is a cheap mask
	sfRingMask = sfRingSize - 1

	// evProgram flags a program-change event; the low byte then carries the
	// GM program number instead of a note. Kept out of the velocity bits so
	// packNote's layout is reused untouched.
	evProgram = 1 << 9
)

type SFSynth struct {
	LevelDB *dsp.ParamCell // output trim in dBFS, applied after Render

	// Event ring — identical discipline to PolySynth: producers serialize on
	// mu, the audio thread drains with atomics only. Program changes ride the
	// same ring as notes so "switch to strings, then play" applies in order.
	mu   sync.Mutex
	ring [sfRingSize]atomic.Uint64
	head atomic.Uint64
	tail atomic.Uint64

	// synth is audio-thread-owned after construction; the control plane never
	// touches it directly (all mutations travel through the ring).
	synth *meltysynth.Synthesizer

	// program mirrors the last requested GM program for snapshots/UI. It is
	// display state, not audio state — the authoritative value lives in the
	// synthesizer once the ring event drains.
	program atomic.Int32

	path string // bank file path, for scene capture and the UI label
}

// bankCache shares loaded SoundFonts across channels. A bank is immutable
// after parsing (meltysynth only reads it), and the big ones are 100-200 MB
// of sample data — three channels playing MuseScore_General must not triple
// that. The cache is process-lifetime by design: banks are few and reused,
// so eviction would only add complexity.
var (
	bankMu    sync.Mutex
	bankCache = map[string]*meltysynth.SoundFont{}
)

func loadBank(path string) (*meltysynth.SoundFont, error) {
	key := filepath.Clean(path)
	bankMu.Lock()
	defer bankMu.Unlock()
	if sf, hit := bankCache[key]; hit {
		return sf, nil
	}
	f, err := os.Open(key)
	if err != nil {
		return nil, serr.Wrap(err, "path", key)
	}
	defer f.Close()
	sf, err := meltysynth.NewSoundFont(f)
	if err != nil {
		return nil, serr.Wrap(err, "msg", "parsing SoundFont", "path", key)
	}
	bankCache[key] = sf
	return sf, nil
}

// NewSFSynth loads (or reuses) the bank at path and builds a synthesizer for
// it. program selects the initial GM program (0 = Acoustic Grand Piano).
// Construction happens on the control plane, so the potentially slow bank
// parse (a couple hundred ms for the large banks) never blocks audio.
func NewSFSynth(path string, sampleRate, levelDB float64, program int) (*SFSynth, error) {
	sf, err := loadBank(path)
	if err != nil {
		return nil, serr.Wrap(err)
	}

	settings := meltysynth.NewSynthesizerSettings(int32(sampleRate))
	// Defaults kept: 64-frame internal blocks (Render slices our 256-frame
	// engine blocks cleanly) and 64-voice polyphony with built-in stealing.
	// Reverb/chorus stay enabled because GM banks are voiced expecting them;
	// the channel strip's own reverb remains available on top.
	synth, err := meltysynth.NewSynthesizer(sf, settings)
	if err != nil {
		return nil, serr.Wrap(err, "msg", "creating SoundFont synthesizer", "path", path)
	}

	s := &SFSynth{
		LevelDB: dsp.NewParam(levelDB),
		synth:   synth,
		path:    path,
	}
	// Safe to poke the synthesizer directly here: it is not yet visible to
	// the audio thread, so this is plain single-threaded setup.
	if program < 0 || program > 127 {
		program = 0
	}
	s.program.Store(int32(program))
	synth.ProcessMidiMessage(0, 0xC0, int32(program), 0)
	return s, nil
}

// NoteOn queues a note-on (MIDI note number, velocity 0..1). Control plane.
func (s *SFSynth) NoteOn(note int, vel float64) {
	if vel <= 0 {
		vel = 0.8 // clicks/taps without pressure data still deserve a solid strike
	}
	s.push(packNote(note, true, vel))
}

// NoteOff queues a note-off. Control plane.
func (s *SFSynth) NoteOff(note int) { s.push(packNote(note, false, 0)) }

// SetProgram queues a GM program (instrument) change 0..127. Control plane.
// Riding the event ring (rather than poking the synthesizer) keeps the
// "audio thread owns the synth" invariant and preserves ordering with notes.
func (s *SFSynth) SetProgram(program int) {
	if program < 0 || program > 127 {
		return
	}
	s.program.Store(int32(program))
	s.push(uint64(program)&0xFF | evProgram)
}

// Program reports the last requested GM program (for snapshots/UI).
func (s *SFSynth) Program() int { return int(s.program.Load()) }

// Path reports the bank file backing this synth (for scene capture).
func (s *SFSynth) Path() string { return s.path }

func (s *SFSynth) push(ev uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tail.Load()
	if t-s.head.Load() >= sfRingSize {
		return // full ring: drop rather than stall the control plane
	}
	s.ring[t&sfRingMask].Store(ev)
	s.tail.Store(t + 1) // publish after the slot is written
}

// drainEvents applies queued events to the synthesizer. Audio thread.
func (s *SFSynth) drainEvents() {
	h := s.head.Load()
	t := s.tail.Load()
	for ; h < t; h++ {
		ev := s.ring[h&sfRingMask].Load()
		if ev&evProgram != 0 {
			// 0xC0 = MIDI program change; channel 0 is the only one we drive.
			s.synth.ProcessMidiMessage(0, 0xC0, int32(ev&0xFF), 0)
			continue
		}
		note, on, vel := unpackNote(ev)
		if on {
			// meltysynth expects MIDI 1..127; round-up keeps pianissimo
			// touches (vel just above 0) audible instead of truncating to 0
			// (which meltysynth treats as note-off).
			v := int32(vel*126) + 1
			s.synth.NoteOn(0, int32(note), v)
		} else {
			s.synth.NoteOff(0, int32(note))
		}
	}
	s.head.Store(h)
}

// Read renders one block. Audio thread — meltysynth's render path is
// allocation- and lock-free (verified against its source), so the realtime
// contract holds end to end.
func (s *SFSynth) Read(l, r []float32) int {
	s.drainEvents()
	s.synth.Render(l, r) // fills (not mixes) the full buffers

	amp := float32(dsp.DBToLin(s.LevelDB.Get()))
	for i := range l {
		l[i] *= amp
		r[i] *= amp
	}
	return len(l)
}

func (s *SFSynth) Name() string { return "sfont:" + filepath.Base(s.path) }
