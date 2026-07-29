package source

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/rohanthewiz/go-daw/dsp"
)

// PolySynth is a polyphonic piano-style synth driven by note-on/off events
// from the web UI (virtual piano). It follows the project's realtime contract:
// the audio thread never allocates, locks, or logs, so all note events travel
// through a fixed-size lock-free ring — HTTP handlers produce, Read consumes.
//
//	control plane (HTTP)             audio thread (Read)
//	NoteOn/NoteOff --mu--> ring[] --atomics--> drain -> voice pool -> mix
//
// The producer side is serialized with a mutex because multiple HTTP requests
// can land concurrently; that lock is never touched by the audio thread, so
// the realtime guarantee holds. Publication is safe because the slot value is
// stored before the tail index is advanced (both atomically), so the consumer
// can never observe an unwritten slot.
const (
	synthVoices   = 16  // fixed pool; 17th simultaneous note steals the oldest voice
	synthRingSize = 128 // power of two so index wrap is a cheap mask
	synthRingMask = synthRingSize - 1
)

// Voice envelope states. A piano has no sustain segment: the note decays
// while held, and note-off only switches to a faster release curve.
const (
	voiceIdle = iota
	voiceAttack
	voiceDecay
	voiceRelease
)

type voice struct {
	state int
	note  int
	held  bool // true until note-off; lets release target only held voices

	phase float64 // 0..1 cycle position, survives blocks (no phase clicks)
	inc   float64 // phase increment per sample (freq / sampleRate)
	env   float64 // current envelope level 0..1
	vel   float64 // strike velocity 0..1, scales amplitude
	gl,gr float64 // per-voice constant-power pan gains (low notes lean left)

	decayCoef float64 // per-voice: high notes decay faster, like real strings
	age       uint64  // monotonic note-on counter for oldest-voice stealing
}

type PolySynth struct {
	LevelDB *dsp.ParamCell // overall output level in dBFS

	// Note-event ring. Events are packed into uint64s (see packNote) so a
	// slot can be stored with a single atomic write — no per-slot locking.
	mu   sync.Mutex // producers only; audio thread never acquires it
	ring [synthRingSize]atomic.Uint64
	head atomic.Uint64 // consumer (audio thread) read index
	tail atomic.Uint64 // producer write index

	voices [synthVoices]voice
	ageCtr uint64 // audio-thread only

	sampleRate  float64
	attackStep  float64 // linear attack: ~3 ms to full level, fast like a hammer strike
	releaseCoef float64 // exponential release: -60 dB in ~150 ms after note-off
}

// NewPolySynth allocates the whole voice pool up front so Read never allocates.
func NewPolySynth(sampleRate, levelDB float64) *PolySynth {
	p := &PolySynth{
		LevelDB:    dsp.NewParam(levelDB),
		sampleRate: sampleRate,
		attackStep: 1 / (0.003 * sampleRate),
		// exp coef c such that c^(t*sr) = 10^-3 (-60 dB) after t seconds.
		releaseCoef: math.Pow(10, -3/(0.15*sampleRate)),
	}
	return p
}

// packNote encodes an event in one uint64: note in bits 0..7, on-flag in
// bit 8, velocity as 16-bit fixed point in bits 16..31.
func packNote(note int, on bool, vel float64) uint64 {
	v := uint64(note) & 0xFF
	if on {
		v |= 1 << 8
	}
	v |= uint64(math.Round(clamp01(vel)*65535)) << 16
	return v
}

func unpackNote(ev uint64) (note int, on bool, vel float64) {
	return int(ev & 0xFF), ev&(1<<8) != 0, float64((ev>>16)&0xFFFF) / 65535
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// NoteOn queues a note-on (MIDI note number, velocity 0..1). Control plane.
func (p *PolySynth) NoteOn(note int, vel float64) { p.push(packNote(note, true, vel)) }

// NoteOff queues a note-off. Control plane.
func (p *PolySynth) NoteOff(note int) { p.push(packNote(note, false, 0)) }

func (p *PolySynth) push(ev uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.tail.Load()
	if t-p.head.Load() >= synthRingSize {
		// Ring full (would need 128 events inside one ~5 ms block). Dropping
		// is the correct failure mode: blocking would stall an HTTP handler,
		// and a lost keypress is preferable to a stalled control plane.
		return
	}
	p.ring[t&synthRingMask].Store(ev)
	p.tail.Store(t + 1) // publish after the slot is written
}

// midiToFreq converts a MIDI note number to Hz (A4 = 69 = 440 Hz).
func midiToFreq(note int) float64 {
	return 440 * math.Pow(2, float64(note-69)/12)
}

// drainEvents applies all queued note events to the voice pool. Audio thread.
// Runs once per block, so key latency is bounded by one block (~5 ms) plus
// device latency — indistinguishable from immediate for a virtual keyboard.
func (p *PolySynth) drainEvents() {
	h := p.head.Load()
	t := p.tail.Load()
	for ; h < t; h++ {
		note, on, vel := unpackNote(p.ring[h&synthRingMask].Load())
		if on {
			p.noteOnVoice(note, vel)
		} else {
			p.noteOffVoice(note)
		}
	}
	p.head.Store(h)
}

func (p *PolySynth) noteOnVoice(note int, vel float64) {
	// Prefer re-striking a voice already sounding this note (avoids the same
	// pitch piling up on two voices), then a free voice, then steal the oldest.
	var v *voice
	for i := range p.voices {
		if p.voices[i].state != voiceIdle && p.voices[i].note == note {
			v = &p.voices[i]
			break
		}
	}
	if v == nil {
		for i := range p.voices {
			if p.voices[i].state == voiceIdle {
				v = &p.voices[i]
				break
			}
		}
	}
	if v == nil {
		oldest := 0
		for i := range p.voices {
			if p.voices[i].age < p.voices[oldest].age {
				oldest = i
			}
		}
		v = &p.voices[oldest]
	}

	if vel <= 0 {
		vel = 0.8
	}
	v.note = note
	v.held = true
	v.vel = vel
	v.inc = midiToFreq(note) / p.sampleRate
	// Attack ramps from the voice's *current* envelope, so re-strikes and
	// stolen voices glide up instead of snapping to zero (click-free).
	v.state = voiceAttack
	p.ageCtr++
	v.age = p.ageCtr

	// Piano-like decay: roughly 8 s at the bottom of the keyboard, halving
	// every two octaves, so high notes ring shorter just like real strings.
	decaySecs := 8 * math.Pow(0.5, float64(note-36)/24)
	if decaySecs < 0.5 {
		decaySecs = 0.5
	}
	v.decayCoef = math.Pow(10, -3/(decaySecs*p.sampleRate))

	// Slight constant-power stereo spread by pitch: low keys left, high keys
	// right, like sitting at the instrument. ±0.4 keeps everything centered
	// enough that the channel's own pan control remains the dominant knob.
	pan := clampF((float64(note)-64)/64*0.4, -0.4, 0.4)
	v.gl = math.Sqrt(0.5 * (1 - pan))
	v.gr = math.Sqrt(0.5 * (1 + pan))
}

func (p *PolySynth) noteOffVoice(note int) {
	for i := range p.voices {
		if p.voices[i].held && p.voices[i].note == note {
			p.voices[i].held = false
			p.voices[i].state = voiceRelease
		}
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Read synthesizes one block. Audio thread — no allocation, locks, or logging.
func (p *PolySynth) Read(l, r []float32) int {
	p.drainEvents()

	for i := range l {
		l[i], r[i] = 0, 0
	}

	amp := dsp.DBToLin(p.LevelDB.Get())

	for vi := range p.voices {
		v := &p.voices[vi]
		if v.state == voiceIdle {
			continue
		}
		for i := range l {
			switch v.state {
			case voiceAttack:
				v.env += p.attackStep
				if v.env >= 1 {
					v.env = 1
					v.state = voiceDecay
				}
			case voiceDecay:
				v.env *= v.decayCoef
			case voiceRelease:
				v.env *= p.releaseCoef
			}
			if v.env < 1e-4 { // below -80 dB: voice is done, park it
				v.state = voiceIdle
				v.env = 0
				break
			}

			// Four-harmonic additive tone. 1/f amplitude rolloff gives a
			// warm, plucked-string timbre; 0.53 normalizes the harmonic sum
			// (1 + .5 + .25 + .12 = 1.87) back to unity peak.
			ph := 2 * math.Pi * v.phase
			s := math.Sin(ph) + 0.5*math.Sin(2*ph) + 0.25*math.Sin(3*ph) + 0.12*math.Sin(4*ph)
			s *= 0.53 * v.env * v.vel * amp

			l[i] += float32(s * v.gl)
			r[i] += float32(s * v.gr)

			v.phase += v.inc
			if v.phase >= 1 {
				v.phase -= 1
			}
		}
	}
	return len(l)
}

// ActiveVoices reports how many voices are sounding (for UI/status). Control
// plane reads of audio-thread state; approximate by design, so no ordering
// guarantees are needed.
func (p *PolySynth) ActiveVoices() int {
	n := 0
	for i := range p.voices {
		if p.voices[i].state != voiceIdle {
			n++
		}
	}
	return n
}

func (p *PolySynth) Name() string { return "synth" }
