package mixer

import (
	"path/filepath"
	"strings"
	"sync"

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

	// recallMu serializes scene recalls (control plane only; the audio
	// thread never touches it). Each recall is many independent writes, so
	// two overlapping recalls (a double-clicked Recall, two tabs) would
	// interleave into a console that matches neither scene.
	recallMu sync.Mutex
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
	DrumKit int     `json:"drumKit,omitempty"` // sfont: percussion kit program (GS numbering; 0 = Standard)
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
			DrumKit: src.DrumKit(),
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
	_, err := c.ApplySceneReport(s)
	return err
}

// ApplySceneReport is ApplyScene plus a list of human-readable warnings for
// the parts it could not restore (a bank or WAV no longer on disk, a plugin
// no longer loaded). A partial recall still succeeds — that is the point of
// skipping bad items — but the caller can now tell the user which channel
// came back silent instead of returning a bare "ok".
func (c *Console) ApplySceneReport(s SceneState) (warnings []string, err error) {
	if s.Version != 1 {
		return nil, serr.New("unsupported scene version")
	}
	c.recallMu.Lock()
	defer c.recallMu.Unlock()

	// Bus gains are clamped to the same -60..+12 dB travel the
	// group/master HTTP handlers enforce: a scene is stored data, and stored
	// data outlives the code that validated it.
	c.Master.GainDB.Set(clampF(s.Master.GainDB, -60, 12))

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
		g.GainDB.Set(clampF(gs.GainDB, -60, 12))
		g.Mute.Store(gs.Mute)
	}

	for i, cs := range s.Channels {
		if i >= len(c.Channels) {
			break
		}
		ch := c.Channels[i]
		ch.SetName(cs.Name)
		if cs.Group >= 0 && cs.Group <= len(c.Groups) {
			ch.GroupIdx.Store(int32(cs.Group))
		}

		applyStrip(ch, cs)

		if w := c.applySource(ch, cs.Source); w != "" {
			warnings = append(warnings, w)
		}
		warnings = append(warnings, c.applyModules(ch, cs.Modules)...)
	}
	return warnings, nil
}

// applyStrip restores every strip parameter through Channel.ApplyParam —
// the same entry point (and so the same clamps) as a live slider move.
//
// Recall used to Set the cells directly, which skipped validation entirely.
// That matters because some out-of-range values are not merely odd but
// destructive: comp.ratio 0 makes the gain computer's slope 1/0, Q near 0
// blows up a biquad, and the resulting NaN latches into the strip. Scenes are
// stored data (and hand-editable JSON), so they get the same treatment as
// any other input. ApplyParam also recomputes biquad coefficients, which the
// old code did by hand.
//
// Each name below is a case in ApplyParam's switch; a typo would surface as
// a logged "unknown channel parameter" rather than a silent skip.
func applyStrip(ch *Channel, cs ChannelState) {
	b := func(on bool) float64 {
		if on {
			return 1
		}
		return 0
	}
	type kv struct {
		name  string
		value float64
	}
	params := []kv{
		{"gain", cs.GainDB},
		{"pan", cs.Pan},
		{"mute", b(cs.Mute)},

		{"gate.enabled", b(cs.Gate.Enabled)},
		{"gate.threshold", cs.Gate.ThresholdDB},
		{"gate.hysteresis", cs.Gate.HysteresisDB},
		{"gate.attack", cs.Gate.AttackMs},
		{"gate.hold", cs.Gate.HoldMs},
		{"gate.release", cs.Gate.ReleaseMs},

		{"hpf.enabled", b(cs.HPF.Enabled)},
		{"hpf.freq", cs.HPF.Freq},
		{"lpf.enabled", b(cs.LPF.Enabled)},
		{"lpf.freq", cs.LPF.Freq},

		{"comp.enabled", b(cs.Comp.Enabled)},
		{"comp.threshold", cs.Comp.ThresholdDB},
		{"comp.ratio", cs.Comp.Ratio},
		{"comp.knee", cs.Comp.KneeDB},
		{"comp.attack", cs.Comp.AttackMs},
		{"comp.release", cs.Comp.ReleaseMs},
		{"comp.makeup", cs.Comp.MakeupDB},

		{"reverb.enabled", b(cs.Reverb.Enabled)},
		{"reverb.predelay", cs.Reverb.PreDelayMs},
		{"reverb.decay", cs.Reverb.Decay},
		{"reverb.damp", cs.Reverb.Damp},
		{"reverb.mix", cs.Reverb.Mix},
	}
	for bi, eqs := range cs.EQ {
		pfx := "eq" + itoa(bi+1) + "."
		params = append(params,
			kv{pfx + "enabled", b(eqs.Enabled)},
			kv{pfx + "freq", eqs.Freq},
			kv{pfx + "gain", eqs.GainDB},
			kv{pfx + "q", eqs.Q},
		)
	}
	for _, p := range params {
		if err := ch.ApplyParam(p.name, p.value); err != nil {
			logger.LogErr(err, "msg", "scene: strip parameter skipped", "channel", itoa(ch.ID))
		}
	}
}

// Source parameter ranges, matching the strip's sliders (web/ui
// channelstrip.go). Enforced on the server because a level is applied as
// DBToLin(level): +1000 dB is 1e50, which overflows float32 to Inf and
// faults the strip. Shared by the live source-param API and SetChannelSource
// (scene recall, source swaps) so both paths agree.
const (
	SourceLevelMinDB = -60.0
	SourceLevelMaxDB = 0.0
	OscFreqMinHz     = 20.0
	OscFreqMaxHz     = 5000.0
)

// ClampSourceLevel bounds a source level (dBFS) to the slider's travel.
func ClampSourceLevel(db float64) float64 { return clampF(db, SourceLevelMinDB, SourceLevelMaxDB) }

// ClampOscFreq bounds the test oscillator's frequency to the slider's travel.
func ClampOscFreq(hz float64) float64 { return clampF(hz, OscFreqMinHz, OscFreqMaxHz) }

// SetChannelSource builds and installs a source from a SourceState — shared
// by scene recall and the live "change source" API.
func (c *Console) SetChannelSource(ch *Channel, ss SourceState) error {
	// Zero means "unset" for both fields (JSON omitempty) and each case
	// below substitutes its own default, so only clamp values that are set.
	if ss.LevelDB != 0 {
		ss.LevelDB = ClampSourceLevel(ss.LevelDB)
	}
	if ss.FreqHz != 0 {
		ss.FreqHz = ClampOscFreq(ss.FreqHz)
	}
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
		if ss.DrumKit != 0 {
			// Zero doubles as "unset" (JSON omitempty) — harmless here, since
			// kit 0 (Standard) is also the synth's channel-9 default.
			sf.SetDrumKit(ss.DrumKit)
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

// applySource restores one channel's source; on failure the channel is
// left silent and a user-facing warning is returned ("" on success).
func (c *Console) applySource(ch *Channel, ss SourceState) string {
	if err := c.SetChannelSource(ch, ss); err != nil {
		logger.LogErr(err, "msg", "scene: source restore failed; channel left silent")
		ch.SetSource(nil)
		what := ss.Type
		if ss.Path != "" {
			what += " " + filepath.Base(ss.Path)
		}
		return ch.Name() + ": " + what + " could not be loaded; channel left silent"
	}
	return ""
}

// applyModules rebuilds one channel's insert chain, skipping (and
// reporting) modules that can no longer be created.
func (c *Console) applyModules(ch *Channel, states []ModuleState) (warnings []string) {
	chain := make([]module.AudioModule, 0, len(states))
	for _, ms := range states {
		m, err := module.Create(ms.Name)
		if err != nil {
			logger.LogErr(err, "msg", "scene: module unavailable; skipping")
			warnings = append(warnings, ch.Name()+": module "+ms.Name+" is not available; skipped")
			continue
		}
		if err := m.Init(c.SampleRate, c.MaxBlock); err != nil {
			logger.LogErr(err, "msg", "scene: module init failed; skipping", "module", ms.Name)
			warnings = append(warnings, ch.Name()+": module "+ms.Name+" failed to initialize; skipped")
			continue
		}
		for id, v := range ms.Params {
			if err := SetModuleParam(m, id, v); err != nil {
				logger.LogErr(err, "msg", "scene: module parameter skipped")
			}
		}
		chain = append(chain, m)
	}
	ch.SetModules(chain)
	return warnings
}
