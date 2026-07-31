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

// MidiPlayer plays a Standard MIDI File (.mid) through a SoundFont bank via
// meltysynth's MidiFileSequencer. It is the "tape deck" sibling of SFSynth:
// same bank cache, same event-ring discipline, but instead of live note events
// the audio thread pulls a pre-parsed message stream from the sequencer.
//
//	control plane (HTTP)                       audio thread (Read)
//	Play/Pause/Stop/SetLoop --mu--> ring[] --atomics--> drain -> seq.Render
//
// Both heavyweight inputs (the bank parse and the .mid parse) happen at
// construction on the control plane; a file or bank change therefore rebuilds
// the source, exactly like a WAV path or sfont bank change. The sequencer's
// render path was verified realtime-safe: processEvents walks pre-parsed
// slices, and Play/Stop only touch pre-allocated synthesizer state.
const (
	midiRingSize = 16 // transport commands are rare; a small ring is plenty
	midiRingMask = midiRingSize - 1

	// Transport commands are mutually exclusive, so plain enum values (not
	// bit flags) ride the ring. 0 is reserved as "empty slot" sentinel-free
	// padding — every real event is nonzero.
	midiEvPlay  = 1
	midiEvPause = 2
	midiEvStop  = 3

	// midiTailSeconds is how long past the last MIDI event the player keeps
	// rendering before auto-stopping (non-loop mode). Two seconds covers
	// typical release/reverb tails so the ending never gets clipped.
	midiTailSeconds = 2
)

// Play states mirrored for the UI. Single-writer: only the audio thread
// stores these (state changes happen where the sequencer lives); the control
// plane just reads.
const (
	midiStateStopped = 0
	midiStatePlaying = 1
	midiStatePaused  = 2
)

type MidiPlayer struct {
	LevelDB *dsp.ParamCell // output trim in dBFS, applied after Render

	// Event ring — same discipline as SFSynth: producers serialize on mu,
	// the audio thread drains with atomics only.
	mu   sync.Mutex
	ring [midiRingSize]atomic.Uint64
	head atomic.Uint64
	tail atomic.Uint64

	// synth/seq/file are audio-thread-owned after construction. The sequencer
	// wraps the synthesizer; pausing renders the synth directly so release
	// tails ring out without the sequencer's clock advancing.
	synth *meltysynth.Synthesizer
	seq   *meltysynth.MidiFileSequencer
	file  *meltysynth.MidiFile

	// loop is read by the audio thread when a play command drains — the
	// sequencer only accepts the loop flag at Play time (its internal field
	// is private), so a mid-song toggle takes effect on the next play.
	loop atomic.Bool

	// state/posFrames are UI mirrors written exclusively by the audio thread
	// (single-writer, so plain Load/Store need no CAS). posFrames counts
	// frames rendered since the last play-from-top.
	state     atomic.Int32
	posFrames atomic.Uint64

	lengthFrames uint64  // song length in engine frames (from MidiFile.GetLength)
	tailFrames   uint64  // extra frames rendered before auto-stop
	lengthSec    float64 // cached for the UI; avoids recomputing per broadcast
	sampleRate   float64

	bankPath string // .sf2 backing the playback, for scene capture and UI
	midiPath string // .mid being played, for scene capture and UI
}

// NewMidiPlayer parses the .mid at midiPath and prepares a sequencer over the
// bank at bankPath (shared through the same process-wide bank cache SFSynth
// uses, so a channel playing a file and a channel playing live notes off the
// same bank cost one copy of the sample data). The player starts stopped —
// scene recall must never surprise-blast a song.
func NewMidiPlayer(bankPath, midiPath string, sampleRate, levelDB float64) (*MidiPlayer, error) {
	sf, err := loadBank(bankPath)
	if err != nil {
		return nil, serr.Wrap(err)
	}

	f, err := os.Open(midiPath)
	if err != nil {
		return nil, serr.Wrap(err, "path", midiPath)
	}
	defer f.Close()
	mf, err := meltysynth.NewMidiFile(f)
	if err != nil {
		return nil, serr.Wrap(err, "msg", "parsing MIDI file", "path", midiPath)
	}

	settings := meltysynth.NewSynthesizerSettings(int32(sampleRate))
	synth, err := meltysynth.NewSynthesizer(sf, settings)
	if err != nil {
		return nil, serr.Wrap(err, "msg", "creating SoundFont synthesizer", "bank", bankPath)
	}

	lengthSec := mf.GetLength().Seconds()
	return &MidiPlayer{
		LevelDB:      dsp.NewParam(levelDB),
		synth:        synth,
		seq:          meltysynth.NewMidiFileSequencer(synth),
		file:         mf,
		lengthFrames: uint64(lengthSec * sampleRate),
		tailFrames:   uint64(midiTailSeconds * sampleRate),
		lengthSec:    lengthSec,
		sampleRate:   sampleRate,
		bankPath:     bankPath,
		midiPath:     midiPath,
	}, nil
}

// Play starts the song from the top, or resumes it if paused. Control plane.
func (p *MidiPlayer) Play() { p.push(midiEvPlay) }

// Pause halts the sequencer clock, letting sounding notes ring out naturally.
// Control plane. A subsequent Play resumes from the pause point.
func (p *MidiPlayer) Pause() { p.push(midiEvPause) }

// Stop resets the song to the top and silences the synthesizer. Control plane.
func (p *MidiPlayer) Stop() { p.push(midiEvStop) }

// SetLoop selects whether the song restarts when it ends. Control plane.
// Takes effect on the next play (the sequencer latches the flag at Play time).
func (p *MidiPlayer) SetLoop(on bool) { p.loop.Store(on) }

// Loop reports the requested loop mode (for snapshots/UI).
func (p *MidiPlayer) Loop() bool { return p.loop.Load() }

// PlayState reports the transport state for the UI: "stopped", "playing",
// or "paused".
func (p *MidiPlayer) PlayState() string {
	switch p.state.Load() {
	case midiStatePlaying:
		return "playing"
	case midiStatePaused:
		return "paused"
	default:
		return "stopped"
	}
}

// LengthSeconds reports the song duration.
func (p *MidiPlayer) LengthSeconds() float64 { return p.lengthSec }

// PosSeconds reports the current playback position. When looping, the frame
// counter keeps accumulating across passes (the sequencer's own clock is
// private), so the position is folded back into the song length here.
func (p *MidiPlayer) PosSeconds() float64 {
	pos := p.posFrames.Load()
	if p.loop.Load() && p.lengthFrames > 0 {
		pos %= p.lengthFrames
	} else if pos > p.lengthFrames {
		pos = p.lengthFrames // clamp the tail so the readout parks at the end
	}
	return float64(pos) / p.sampleRate
}

// BankPath reports the .sf2 backing playback (for scene capture).
func (p *MidiPlayer) BankPath() string { return p.bankPath }

// MidiPath reports the .mid being played (for scene capture).
func (p *MidiPlayer) MidiPath() string { return p.midiPath }

func (p *MidiPlayer) push(ev uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.tail.Load()
	if t-p.head.Load() >= midiRingSize {
		return // full ring: drop rather than stall the control plane
	}
	p.ring[t&midiRingMask].Store(ev)
	p.tail.Store(t + 1) // publish after the slot is written
}

// drainEvents applies queued transport commands. Audio thread.
func (p *MidiPlayer) drainEvents() {
	h := p.head.Load()
	t := p.tail.Load()
	for ; h < t; h++ {
		switch p.ring[h&midiRingMask].Load() {
		case midiEvPlay:
			if p.state.Load() == midiStatePaused {
				// Resume: the sequencer's clock and message index survived the
				// pause untouched, so just letting Render run again continues
				// exactly where the song left off. Note-offs for keys released
				// during the pause arrive as no-ops — harmless.
				p.state.Store(midiStatePlaying)
			} else {
				// Play latches the loop flag and resets the synthesizer —
				// both alloc-free, verified against meltysynth's source.
				p.seq.Play(p.file, p.loop.Load())
				p.posFrames.Store(0)
				p.state.Store(midiStatePlaying)
			}
		case midiEvPause:
			if p.state.Load() == midiStatePlaying {
				// Release (not kill) sounding notes so the pause sounds like
				// lifting hands off the keys, not yanking the power cord.
				p.synth.NoteOffAll(false)
				p.state.Store(midiStatePaused)
			}
		case midiEvStop:
			p.seq.Stop() // resets the synth: immediate silence, song back to top
			p.posFrames.Store(0)
			p.state.Store(midiStateStopped)
		}
	}
	p.head.Store(h)
}

// Read renders one block. Audio thread — the sequencer's event walk and the
// synthesizer render are both allocation- and lock-free.
func (p *MidiPlayer) Read(l, r []float32) int {
	p.drainEvents()

	switch p.state.Load() {
	case midiStatePlaying:
		p.seq.Render(l, r) // fills (not mixes) and advances the song clock
		pos := p.posFrames.Add(uint64(len(l)))
		// Auto-stop once the song plus a tail window has rendered (non-loop):
		// without this the transport would show "playing" forever over silence.
		if !p.loop.Load() && pos > p.lengthFrames+p.tailFrames {
			p.seq.Stop()
			p.posFrames.Store(0)
			p.state.Store(midiStateStopped)
		}
	case midiStatePaused:
		// Render the synth directly (sequencer clock frozen) so release and
		// reverb tails finish decaying instead of freezing mid-ring.
		p.synth.Render(l, r)
	default:
		return 0 // stopped: engine zero-fills, no render cost
	}

	amp := float32(dsp.DBToLin(p.LevelDB.Get()))
	for i := range l {
		l[i] *= amp
		r[i] *= amp
	}
	return len(l)
}

func (p *MidiPlayer) Name() string { return "midi:" + filepath.Base(p.midiPath) }
