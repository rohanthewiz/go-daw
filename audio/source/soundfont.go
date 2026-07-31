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

	// evPedal flags a sustain-pedal event (MIDI CC64); bit 0 carries the
	// pedal state (1 = down). Riding the ring rather than poking the synth
	// keeps ordering with notes: "release keys, lift pedal" must apply in
	// exactly that order or notes cut off early.
	evPedal = 1 << 10

	// evDrums flags a percussion-mode toggle; bit 0 carries the new state.
	// Drums mode reroutes subsequent note events to MIDI channel 9, which
	// meltysynth fixes to the bank's percussion presets (GM convention).
	evDrums = 1 << 11

	// evDrumKit flags a drum-kit change; the low byte carries the kit's
	// program number (GS convention: 0 Standard, 8 Room, 16 Power, 24
	// Electronic, 25 TR-808, 32 Jazz, 40 Brush, 48 Orchestra, 56 SFX).
	// Distinct from evProgram because the two target different channels:
	// kit changes are program changes on the percussion channel (bank 128),
	// where presets are selected per-kit rather than per-instrument.
	evDrumKit = 1 << 12
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

	// drumKit mirrors the last requested percussion kit for snapshots/UI —
	// display state only, like program; the authoritative value lives in the
	// synthesizer's channel-9 patch once the ring event drains.
	drumKit atomic.Int32

	// drums mirrors the last requested percussion state for snapshots/UI —
	// same display-only role as program. The audio-thread copy (drumsOn) is
	// what actually routes notes, and it only changes via the ring.
	drums atomic.Bool

	// drumsOn is the audio thread's routing switch: false → melodic channel 0,
	// true → percussion channel 9. Audio-thread-owned; never read or written
	// by the control plane (that's what the drums atomic above is for).
	drumsOn bool

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

// Sustain queues a sustain-pedal change (down = pedal pressed). Control plane.
// Implements Sustainer; meltysynth honors CC64 natively — voices whose keys
// are released while the pedal is down keep ringing until it lifts.
func (s *SFSynth) Sustain(down bool) {
	var ev uint64 = evPedal
	if down {
		ev |= 1
	}
	s.push(ev)
}

// SetDrums queues a percussion-mode toggle. Control plane. When enabled, note
// events target MIDI channel 9, so the keyboard plays the bank's GM drum kit
// (note 36 = kick, 38 = snare, 42 = closed hat, …) instead of the melodic
// program.
func (s *SFSynth) SetDrums(on bool) {
	s.drums.Store(on)
	var ev uint64 = evDrums
	if on {
		ev |= 1
	}
	s.push(ev)
}

// Drums reports the last requested percussion state (for snapshots/UI).
func (s *SFSynth) Drums() bool { return s.drums.Load() }

// SetDrumKit queues a drum-kit change 0..127 (a program change on the
// percussion channel). Control plane. Kits follow the GS numbering most GM
// banks use (0 Standard, 8 Room, 16 Power, …); a kit the loaded bank lacks
// falls back to meltysynth's default preset, so a bad pick can't go silent.
func (s *SFSynth) SetDrumKit(kit int) {
	if kit < 0 || kit > 127 {
		return
	}
	s.drumKit.Store(int32(kit))
	s.push(uint64(kit)&0xFF | evDrumKit)
}

// DrumKit reports the last requested drum kit (for snapshots/UI).
func (s *SFSynth) DrumKit() int { return int(s.drumKit.Load()) }

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
		switch {
		case ev&evProgram != 0:
			// 0xC0 = MIDI program change; always the melodic channel — the
			// percussion channel's presets are keyed by note, not program.
			s.synth.ProcessMidiMessage(0, 0xC0, int32(ev&0xFF), 0)
		case ev&evPedal != 0:
			// 0xB0/64 = CC64 hold pedal. Melodic channel only: GM percussion
			// is one-shot by convention, so sustaining it would just smear
			// cymbal tails unnaturally.
			var val int32
			if ev&1 != 0 {
				val = 127
			}
			s.synth.ProcessMidiMessage(0, 0xB0, 64, val)
		case ev&evDrums != 0:
			// Channel switch mid-hold would send note-offs to the wrong
			// channel and strand notes; releasing everything first (with
			// normal release tails, not a hard cut) makes the toggle safe at
			// any moment.
			s.synth.NoteOffAll(false)
			s.drumsOn = ev&1 != 0
		case ev&evDrumKit != 0:
			// 0xC0 on channel 9 = kit select (the channel is pinned to bank
			// 128, so the patch number picks among percussion presets). No
			// note-off sweep needed: already-sounding drum hits are one-shots
			// that finish on the old kit's samples, which is what a hardware
			// module does too.
			s.synth.ProcessMidiMessage(9, 0xC0, int32(ev&0xFF), 0)
		default:
			ch := int32(0)
			if s.drumsOn {
				ch = 9 // GM percussion channel; meltysynth pins it to the drum bank
			}
			note, on, vel := unpackNote(ev)
			if on {
				// meltysynth expects MIDI 1..127; round-up keeps pianissimo
				// touches (vel just above 0) audible instead of truncating to 0
				// (which meltysynth treats as note-off).
				v := int32(vel*126) + 1
				s.synth.NoteOn(ch, int32(note), v)
			} else {
				s.synth.NoteOff(ch, int32(note))
			}
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
