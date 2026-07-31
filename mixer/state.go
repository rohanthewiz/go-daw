package mixer

import (
	"strings"

	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// Console is the whole mixing surface: channels, groups, master. The audio
// engine drives it block by block; the web layer reads/writes its cells; the
// scene system snapshots and restores it. Keeping it a plain struct (rather
// than burying it inside the engine) lets the store and web packages depend
// on mixer without dragging in the audio device code.
type Console struct {
	SampleRate float64
	MaxBlock   int
	Channels   []*Channel
	Groups     []*Group
	Master     *Master

	// LiveFeed is the engine's capture buffer; needed here so ApplyScene can
	// reconstruct "live" sources.
	LiveFeed *source.LiveFeed
}

// NewConsole builds the full surface.
func NewConsole(numChannels, numGroups int, sampleRate float64, maxBlock int) *Console {
	c := &Console{
		SampleRate: sampleRate,
		MaxBlock:   maxBlock,
		Master:     NewMaster(sampleRate, maxBlock),
		LiveFeed:   &source.LiveFeed{L: make([]float32, maxBlock), R: make([]float32, maxBlock)},
	}
	for i := 1; i <= numChannels; i++ {
		c.Channels = append(c.Channels, NewChannel(i, sampleRate, maxBlock))
	}
	for i := 1; i <= numGroups; i++ {
		c.Groups = append(c.Groups, NewGroup(i, maxBlock))
	}
	return c
}

// ---- Scene state ("digital memory") ----
// These structs are the JSON wire/persistence format. Version is bumped on
// breaking shape changes so old scenes can be migrated or rejected cleanly.

type SceneState struct {
	Version  int            `json:"version"`
	Master   MasterState    `json:"master"`
	Groups   []GroupState   `json:"groups"`
	Channels []ChannelState `json:"channels"`
}

type MasterState struct {
	GainDB float64 `json:"gainDb"`
}

type GroupState struct {
	Name   string  `json:"name"`
	GainDB float64 `json:"gainDb"`
	Mute   bool    `json:"mute"`
}

type ChannelState struct {
	Name   string  `json:"name"`
	GainDB float64 `json:"gainDb"`
	Pan    float64 `json:"pan"`
	Mute   bool    `json:"mute"`
	Group  int     `json:"group"` // 0 = direct to master

	Gate   GateState     `json:"gate"`
	HPF    FilterState   `json:"hpf"`
	LPF    FilterState   `json:"lpf"`
	EQ     [3]EQBandState `json:"eq"`
	Comp   CompState     `json:"comp"`
	Reverb ReverbState   `json:"reverb"`

	Source  SourceState   `json:"source"`
	Modules []ModuleState `json:"modules"`
}

type GateState struct {
	Enabled      bool    `json:"enabled"`
	ThresholdDB  float64 `json:"thresholdDb"`
	HysteresisDB float64 `json:"hysteresisDb"`
	AttackMs     float64 `json:"attackMs"`
	HoldMs       float64 `json:"holdMs"`
	ReleaseMs    float64 `json:"releaseMs"`
}

type FilterState struct {
	Enabled bool    `json:"enabled"`
	Freq    float64 `json:"freq"`
}

type EQBandState struct {
	Enabled bool    `json:"enabled"`
	Freq    float64 `json:"freq"`
	GainDB  float64 `json:"gainDb"`
	Q       float64 `json:"q"`
}

type CompState struct {
	Enabled     bool    `json:"enabled"`
	ThresholdDB float64 `json:"thresholdDb"`
	Ratio       float64 `json:"ratio"`
	KneeDB      float64 `json:"kneeDb"`
	AttackMs    float64 `json:"attackMs"`
	ReleaseMs   float64 `json:"releaseMs"`
	MakeupDB    float64 `json:"makeupDb"`
}

type ReverbState struct {
	Enabled    bool    `json:"enabled"`
	PreDelayMs float64 `json:"preDelayMs"`
	Decay      float64 `json:"decay"`
	Damp       float64 `json:"damp"`
	Mix        float64 `json:"mix"`
}

type SourceState struct {
	Type    string  `json:"type"`           // "none" | "osc" | "wav" | "live" | "synth" | "sfont" | "midi"
	Path    string  `json:"path,omitempty"` // wav: audio file; sfont: .sf2 bank file; midi: .mid song file
	Bank    string  `json:"bank,omitempty"` // midi: .sf2 bank the song plays through
	FreqHz  float64 `json:"freqHz,omitempty"`
	LevelDB float64 `json:"levelDb,omitempty"`
	Square  bool    `json:"square,omitempty"`
	Program int     `json:"program,omitempty"` // sfont: GM program 0..127
	Drums   bool    `json:"drums,omitempty"`   // sfont: route notes to the GM percussion channel
	Loop    bool    `json:"loop,omitempty"`    // midi: restart the song when it ends
}

type ModuleState struct {
	Name   string             `json:"name"`
	Params map[string]float64 `json:"params"`
}

// Snapshot captures the entire console into a SceneState. Control plane —
// reading ParamCells is wait-free and safe at any time. The snapshot is not
// sample-accurate across parameters (each cell is read independently), which
// is fine: scenes capture a mix, not an instant.
func (c *Console) Snapshot() SceneState {
	s := SceneState{
		Version: 1,
		Master:  MasterState{GainDB: c.Master.GainDB.Get()},
	}
	for _, g := range c.Groups {
		s.Groups = append(s.Groups, GroupState{
			Name:   g.Name(),
			GainDB: g.GainDB.Get(),
			Mute:   g.Mute.Load(),
		})
	}
	for _, ch := range c.Channels {
		cs := ChannelState{
			Name:   ch.Name(),
			GainDB: ch.GainDB.Get(),
			Pan:    ch.Pan.Get(),
			Mute:   ch.Mute.Load(),
			Group:  int(ch.GroupIdx.Load()),
			Gate: GateState{
				Enabled:      ch.Gate.Enabled.Load(),
				ThresholdDB:  ch.Gate.OpenThreshDB.Get(),
				HysteresisDB: ch.Gate.HysteresisDB.Get(),
				AttackMs:     ch.Gate.AttackMs.Get(),
				HoldMs:       ch.Gate.HoldMs.Get(),
				ReleaseMs:    ch.Gate.ReleaseMs.Get(),
			},
			HPF: FilterState{Enabled: ch.HPF.Enabled.Load(), Freq: ch.HPF.Freq.Get()},
			LPF: FilterState{Enabled: ch.LPF.Enabled.Load(), Freq: ch.LPF.Freq.Get()},
			Comp: CompState{
				Enabled:     ch.Comp.Enabled.Load(),
				ThresholdDB: ch.Comp.ThresholdDB.Get(),
				Ratio:       ch.Comp.Ratio.Get(),
				KneeDB:      ch.Comp.KneeDB.Get(),
				AttackMs:    ch.Comp.AttackMs.Get(),
				ReleaseMs:   ch.Comp.ReleaseMs.Get(),
				MakeupDB:    ch.Comp.MakeupDB.Get(),
			},
			Reverb: ReverbState{
				Enabled:    ch.Reverb.Enabled.Load(),
				PreDelayMs: ch.Reverb.PreDelayMs.Get(),
				Decay:      ch.Reverb.Decay.Get(),
				Damp:       ch.Reverb.Damp.Get(),
				Mix:        ch.Reverb.Mix.Get(),
			},
			Source: captureSource(ch.Source()),
		}
		for i, eq := range ch.EQ {
			cs.EQ[i] = EQBandState{
				Enabled: eq.Enabled.Load(),
				Freq:    eq.Freq.Get(),
				GainDB:  eq.GainDB.Get(),
				Q:       eq.Q.Get(),
			}
		}
		for _, m := range ch.Modules() {
			ms := ModuleState{Name: m.Name(), Params: map[string]float64{}}
			for _, spec := range m.Params() {
				ms.Params[spec.ID] = m.GetParam(spec.ID)
			}
			cs.Modules = append(cs.Modules, ms)
		}
		s.Channels = append(s.Channels, cs)
	}
	return s
}

func captureSource(s source.Source) SourceState {
	switch src := s.(type) {
	case *source.Oscillator:
		return SourceState{
			Type:    "osc",
			FreqHz:  src.FreqHz.Get(),
			LevelDB: src.LevelDB.Get(),
			Square:  src.Wave() == source.WaveSquare,
		}
	case *source.WavSource:
		return SourceState{Type: "wav", Path: strings.TrimPrefix(src.Name(), "wav:")}
	case *source.PolySynth:
		// Note state is performance, not mix — only the level is scene-worthy.
		return SourceState{Type: "synth", LevelDB: src.LevelDB.Get()}
	case *source.SFSynth:
		// Same reasoning as synth, plus the bank path and instrument so a
		// recalled scene comes back with the exact same sound.
		return SourceState{
			Type:    "sfont",
			Path:    src.Path(),
			Program: src.Program(),
			Drums:   src.Drums(),
			LevelDB: src.LevelDB.Get(),
		}
	case *source.MidiPlayer:
		// Transport position is performance state (like held notes), so only
		// the file/bank pairing and the loop preference are scene-worthy.
		return SourceState{
			Type:    "midi",
			Path:    src.MidiPath(),
			Bank:    src.BankPath(),
			Loop:    src.Loop(),
			LevelDB: src.LevelDB.Get(),
		}
	case *source.LiveSource:
		return SourceState{Type: "live"}
	default:
		return SourceState{Type: "none"}
	}
}

// ApplyScene restores a snapshot. Live-safe by construction: every mutation
// goes through the same atomic cells and copy-on-write pointers the HTTP
// handlers use, so audio keeps flowing (parameters glide to their recalled
// values within a block or two). Errors on individual items (a missing WAV,
// an unknown module) are logged and skipped so one stale reference can't
// abort the rest of the recall.
func (c *Console) ApplyScene(s SceneState) error {
	if s.Version != 1 {
		return serr.New("unsupported scene version")
	}

	c.Master.GainDB.Set(s.Master.GainDB)

	if len(s.Groups) != len(c.Groups) || len(s.Channels) != len(c.Channels) {
		logger.Info("Scene geometry differs from console; applying overlap",
			"sceneChannels", itoa(len(s.Channels)), "consoleChannels", itoa(len(c.Channels)))
	}

	for i, gs := range s.Groups {
		if i >= len(c.Groups) {
			break
		}
		g := c.Groups[i]
		g.SetName(gs.Name)
		g.GainDB.Set(gs.GainDB)
		g.Mute.Store(gs.Mute)
	}

	for i, cs := range s.Channels {
		if i >= len(c.Channels) {
			break
		}
		ch := c.Channels[i]
		ch.SetName(cs.Name)
		ch.GainDB.Set(cs.GainDB)
		ch.Pan.Set(cs.Pan)
		ch.Mute.Store(cs.Mute)
		if cs.Group >= 0 && cs.Group <= len(c.Groups) {
			ch.GroupIdx.Store(int32(cs.Group))
		}

		ch.Gate.Enabled.Store(cs.Gate.Enabled)
		ch.Gate.OpenThreshDB.Set(cs.Gate.ThresholdDB)
		ch.Gate.HysteresisDB.Set(cs.Gate.HysteresisDB)
		ch.Gate.AttackMs.Set(cs.Gate.AttackMs)
		ch.Gate.HoldMs.Set(cs.Gate.HoldMs)
		ch.Gate.ReleaseMs.Set(cs.Gate.ReleaseMs)

		ch.HPF.Enabled.Store(cs.HPF.Enabled)
		ch.HPF.Freq.Set(cs.HPF.Freq)
		ch.HPF.Update()
		ch.LPF.Enabled.Store(cs.LPF.Enabled)
		ch.LPF.Freq.Set(cs.LPF.Freq)
		ch.LPF.Update()

		for bi, eqs := range cs.EQ {
			eq := ch.EQ[bi]
			eq.Enabled.Store(eqs.Enabled)
			eq.Freq.Set(eqs.Freq)
			eq.GainDB.Set(eqs.GainDB)
			eq.Q.Set(eqs.Q)
			eq.Update()
		}

		ch.Comp.Enabled.Store(cs.Comp.Enabled)
		ch.Comp.ThresholdDB.Set(cs.Comp.ThresholdDB)
		ch.Comp.Ratio.Set(cs.Comp.Ratio)
		ch.Comp.KneeDB.Set(cs.Comp.KneeDB)
		ch.Comp.AttackMs.Set(cs.Comp.AttackMs)
		ch.Comp.ReleaseMs.Set(cs.Comp.ReleaseMs)
		ch.Comp.MakeupDB.Set(cs.Comp.MakeupDB)

		ch.Reverb.Enabled.Store(cs.Reverb.Enabled)
		ch.Reverb.PreDelayMs.Set(cs.Reverb.PreDelayMs)
		ch.Reverb.Decay.Set(cs.Reverb.Decay)
		ch.Reverb.Damp.Set(cs.Reverb.Damp)
		ch.Reverb.Mix.Set(cs.Reverb.Mix)

		c.applySource(ch, cs.Source)
		c.applyModules(ch, cs.Modules)
	}
	return nil
}

// SetChannelSource builds and installs a source from a SourceState — shared
// by scene recall and the live "change source" API.
func (c *Console) SetChannelSource(ch *Channel, ss SourceState) error {
	switch ss.Type {
	case "osc":
		freq, level := ss.FreqHz, ss.LevelDB
		if freq == 0 {
			freq = 220
		}
		if level == 0 {
			level = -18
		}
		osc := source.NewOscillator(c.SampleRate, freq, level)
		if ss.Square {
			osc.SetWave(source.WaveSquare)
		}
		ch.SetSource(osc)
	case "synth":
		level := ss.LevelDB
		if level == 0 {
			// Zero doubles as "unset" (JSON omitempty); -12 dBFS leaves
			// headroom for chords — 16 voices at full level would clip hard.
			level = -12
		}
		ch.SetSource(source.NewPolySynth(c.SampleRate, level))
	case "wav":
		ws, err := source.LoadWav(ss.Path, c.SampleRate)
		if err != nil {
			return serr.Wrap(err, "channel", itoa(ch.ID))
		}
		ch.SetSource(ws)
	case "sfont":
		if ss.Path == "" {
			return serr.New("soundfont source requires a bank path", "channel", itoa(ch.ID))
		}
		level := ss.LevelDB
		if level == 0 {
			// Zero doubles as "unset" (JSON omitempty). SF2 banks are already
			// conservatively mixed and meltysynth's master volume sits at 0.5,
			// so only a light trim is needed for chord headroom.
			level = -6
		}
		sf, err := source.NewSFSynth(ss.Path, c.SampleRate, level, ss.Program)
		if err != nil {
			return serr.Wrap(err, "channel", itoa(ch.ID))
		}
		if ss.Drums {
			// Queued through the event ring; it drains on the first Read, so
			// the synth is already in drum mode before any note can arrive.
			sf.SetDrums(true)
		}
		ch.SetSource(sf)
	case "midi":
		if ss.Path == "" || ss.Bank == "" {
			return serr.New("midi source requires a song path and a bank", "channel", itoa(ch.ID))
		}
		level := ss.LevelDB
		if level == 0 {
			// Zero doubles as "unset" (JSON omitempty); same -6 dBFS trim
			// rationale as sfont — banks are conservatively mixed already.
			level = -6
		}
		mp, err := source.NewMidiPlayer(ss.Bank, ss.Path, c.SampleRate, level)
		if err != nil {
			return serr.Wrap(err, "channel", itoa(ch.ID))
		}
		// Loop is a control-plane atomic latched at play time, so setting it
		// here is race-free. The player installs stopped: recalling a scene
		// must never start a song blasting unprompted.
		mp.SetLoop(ss.Loop)
		ch.SetSource(mp)
	case "live":
		ch.SetSource(source.NewLive(c.LiveFeed))
	case "none", "":
		ch.SetSource(nil)
	default:
		return serr.New("unknown source type", "type", ss.Type)
	}
	return nil
}

func (c *Console) applySource(ch *Channel, ss SourceState) {
	if err := c.SetChannelSource(ch, ss); err != nil {
		logger.LogErr(err, "msg", "scene: source restore failed; channel left silent")
		ch.SetSource(nil)
	}
}

func (c *Console) applyModules(ch *Channel, states []ModuleState) {
	chain := make([]module.AudioModule, 0, len(states))
	for _, ms := range states {
		m, err := module.Create(ms.Name)
		if err != nil {
			logger.LogErr(err, "msg", "scene: module unavailable; skipping")
			continue
		}
		if err := m.Init(c.SampleRate, c.MaxBlock); err != nil {
			logger.LogErr(err, "msg", "scene: module init failed; skipping", "module", ms.Name)
			continue
		}
		for id, v := range ms.Params {
			m.SetParam(id, v)
		}
		chain = append(chain, m)
	}
	ch.SetModules(chain)
}
