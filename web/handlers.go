package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/mixer"
	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/go-daw/tutorial"
	"github.com/rohanthewiz/go-daw/web/ui"
	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/rweb"
	"github.com/rohanthewiz/serr"
)

// fail logs once at the handler boundary and returns the HTTP error.
// Centralizing this keeps the "log once, at the edge" rule from the serr
// philosophy intact — inner layers only wrap and bubble.
func fail(ctx rweb.Context, err error, status int) error {
	logger.LogErr(err)
	return ctx.WriteError(err, status)
}

func ok(ctx rweb.Context) error {
	return ctx.WriteJSON(map[string]bool{"ok": true})
}

// channel resolves the :id path param to a mixer channel.
func (srv *Server) channel(ctx rweb.Context) (*mixer.Channel, error) {
	idStr := ctx.Request().Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id < 1 || id > len(srv.engine.Console.Channels) {
		return nil, serr.New("invalid channel id", "id", idStr)
	}
	return srv.engine.Console.Channels[id-1], nil
}

func decodeBody[T any](ctx rweb.Context) (T, error) {
	var v T
	if err := json.Unmarshal(ctx.Request().Body(), &v); err != nil {
		return v, serr.Wrap(err, "msg", "invalid JSON body")
	}
	return v, nil
}

// ---- pages & info ----

func (srv *Server) pageHandler(ctx rweb.Context) error {
	scenes, err := srv.store.List()
	if err != nil {
		logger.LogErr(err, "msg", "listing scenes for page; continuing with none")
	}
	html := ui.MixerPage(ui.PageData{
		Console:    srv.engine.Console,
		Scenes:     scenes,
		Recording:  srv.engine.Recorder() != nil,
		Duplex:     srv.engine.Duplex,
		Soundbanks: srv.listSoundbanks(),
		MidiFiles:  srv.listMidiFiles(),
		MetroBPM:   srv.setting("metro.bpm", "120"),
		MetroBeats: srv.setting("metro.beats", "4"),
	})
	return ctx.WriteHTML(html)
}

// listSoundbanks scans the configured directory for .sf2 files. Scanned per
// page render (not cached) so dropping a new bank into the folder shows up on
// the next reload without a restart; the directory holds a handful of files,
// so the ReadDir cost is noise.
func (srv *Server) listSoundbanks() (banks []string) {
	entries, err := os.ReadDir(srv.cfg.SoundbanksDir)
	if err != nil {
		logger.LogErr(serr.Wrap(err, "dir", srv.cfg.SoundbanksDir), "msg", "scanning soundbanks; offering none")
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".sf2") {
			continue
		}
		banks = append(banks, filepath.Join(srv.cfg.SoundbanksDir, e.Name()))
	}
	return banks
}

// soundfontsHandler exposes the bank list to the client (used after structural
// changes without a full page reload).
func (srv *Server) soundfontsHandler(ctx rweb.Context) error {
	return ctx.WriteJSON(srv.listSoundbanks())
}

// listMidiFiles scans the configured directory for .mid/.midi songs — the
// exact drop-a-file-and-reload discipline listSoundbanks establishes, and for
// the same reason: the folder is tiny, so a per-render ReadDir is noise.
func (srv *Server) listMidiFiles() (files []string) {
	entries, err := os.ReadDir(srv.cfg.MidiDir)
	if err != nil {
		logger.LogErr(serr.Wrap(err, "dir", srv.cfg.MidiDir), "msg", "scanning midi files; offering none")
		return nil
	}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (!strings.EqualFold(ext, ".mid") && !strings.EqualFold(ext, ".midi")) {
			continue
		}
		files = append(files, filepath.Join(srv.cfg.MidiDir, e.Name()))
	}
	return files
}

// midiFilesHandler exposes the song list to the client.
func (srv *Server) midiFilesHandler(ctx rweb.Context) error {
	return ctx.WriteJSON(srv.listMidiFiles())
}

func (srv *Server) stateHandler(ctx rweb.Context) error {
	return ctx.WriteJSON(map[string]any{
		"scene":     srv.engine.Console.Snapshot(),
		"recording": srv.engine.Recorder() != nil,
		"duplex":    srv.engine.Duplex,
		"modules":   module.Available(),
	})
}

func (srv *Server) modulesHandler(ctx rweb.Context) error {
	return ctx.WriteJSON(module.Available())
}

// lessonsHandler serves the built-in tutorial catalog. Lessons are static
// compiled-in data, so the client fetches once at page load and drives the
// whole tutorial from that snapshot.
func (srv *Server) lessonsHandler(ctx rweb.Context) error {
	return ctx.WriteJSON(tutorial.Lessons())
}

// ---- channel parameters ----

type paramReq struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func (srv *Server) channelParamHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[paramReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	if err := ch.ApplyParam(req.Name, req.Value); err != nil {
		return fail(ctx, err, 400)
	}
	return ok(ctx)
}

// sourceParamHandler adjusts live parameters of the current source (today:
// the oscillator's freq/level) without rebuilding it.
func (srv *Server) sourceParamHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[paramReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	switch src := ch.Source().(type) {
	case *source.Oscillator:
		switch req.Name {
		case "osc.freq":
			src.FreqHz.Set(req.Value)
		case "osc.level":
			src.LevelDB.Set(req.Value)
		default:
			return fail(ctx, serr.New("unknown source parameter", "param", req.Name), 400)
		}
	case *source.PolySynth:
		switch req.Name {
		case "synth.level":
			src.LevelDB.Set(req.Value)
		default:
			return fail(ctx, serr.New("unknown source parameter", "param", req.Name), 400)
		}
	case *source.SFSynth:
		switch req.Name {
		case "sfont.level":
			src.LevelDB.Set(req.Value)
		case "sfont.program":
			// Program changes are live (queued through the synth's event ring),
			// so switching instruments never rebuilds the source or drops notes.
			src.SetProgram(int(req.Value))
		case "sfont.drums":
			// Live too: the toggle rides the ring, releasing sounding notes
			// before rerouting to the GM percussion channel.
			src.SetDrums(req.Value >= 0.5)
		case "sfont.kit":
			// Kit select is a program change on the percussion channel — live
			// through the ring like program, so switching kits never rebuilds.
			src.SetDrumKit(int(req.Value))
		default:
			return fail(ctx, serr.New("unknown source parameter", "param", req.Name), 400)
		}
	case *source.MidiPlayer:
		switch req.Name {
		case "midi.level":
			src.LevelDB.Set(req.Value)
		case "midi.loop":
			// Control-plane atomic latched by the audio thread at play time;
			// mid-song toggles apply on the next play (sequencer limitation).
			src.SetLoop(req.Value >= 0.5)
		default:
			return fail(ctx, serr.New("unknown source parameter", "param", req.Name), 400)
		}
	default:
		return fail(ctx, serr.New("channel source has no adjustable parameters"), 400)
	}
	return ok(ctx)
}

// ---- MIDI file transport ----

type midiCtlReq struct {
	Action string `json:"action"` // "play" | "pause" | "stop"
}

// midiControlHandler drives the MidiPlayer transport. A dedicated endpoint
// (rather than riding source-param) because transport commands are discrete
// events with ordering semantics, not continuous values to debounce — the
// same reasoning that keeps note and pedal events on their own routes.
func (srv *Server) midiControlHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[midiCtlReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	mp, isMidi := ch.Source().(*source.MidiPlayer)
	if !isMidi {
		// 409 mirrors note/pedal: wrong source type is a state conflict.
		return fail(ctx, serr.New("channel source is not a midi player", "channel", strconv.Itoa(ch.ID)), 409)
	}
	switch req.Action {
	case "play":
		mp.Play()
	case "pause":
		mp.Pause()
	case "stop":
		mp.Stop()
	default:
		return fail(ctx, serr.New("unknown transport action", "action", req.Action), 400)
	}
	return ok(ctx)
}

// ---- virtual piano ----

type noteReq struct {
	Note     int     `json:"note"`     // MIDI note number 0..127
	On       bool    `json:"on"`       // true = key down, false = key up
	Velocity float64 `json:"velocity"` // 0..1; ignored for note-off
}

// noteHandler feeds virtual-piano key events into the channel's PolySynth.
// Deliberately bypasses the debounced param path: every keypress matters, and
// PolySynth's internal lock-free ring makes each call wait-free with respect
// to the audio thread, so there is nothing to coalesce.
func (srv *Server) noteHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[noteReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	if req.Note < 0 || req.Note > 127 {
		return fail(ctx, serr.New("note out of range", "note", strconv.Itoa(req.Note)), 400)
	}
	// Any NotePlayer will do — PolySynth and SFSynth share the interface, so
	// the piano/tutorial work identically over either instrument.
	syn, isSynth := ch.Source().(source.NotePlayer)
	if !isSynth {
		// 409 (not 400) so the client can distinguish "wrong source type" —
		// e.g. someone swapped the source mid-performance — from a bad request.
		return fail(ctx, serr.New("channel source is not a synth", "channel", strconv.Itoa(ch.ID)), 409)
	}
	if req.On {
		syn.NoteOn(req.Note, req.Velocity)
	} else {
		syn.NoteOff(req.Note)
	}
	return ok(ctx)
}

type pedalReq struct {
	Down bool `json:"down"` // true = pedal pressed
}

// pedalHandler feeds sustain-pedal (CC64) events to the channel's instrument.
// Separate from noteHandler because a pedal event has no note number and a
// different capability check: sources opt in via Sustainer, not NotePlayer.
func (srv *Server) pedalHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[pedalReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	sus, canSustain := ch.Source().(source.Sustainer)
	if !canSustain {
		// 409 mirrors noteHandler: "this source can't do that" is a state
		// conflict, not a malformed request.
		return fail(ctx, serr.New("channel source has no sustain pedal", "channel", strconv.Itoa(ch.ID)), 409)
	}
	sus.Sustain(req.Down)
	return ok(ctx)
}

func (srv *Server) channelSourceHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[mixer.SourceState](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	if req.Type == "live" && !srv.engine.Duplex {
		return fail(ctx, serr.New("live input unavailable (engine is playback-only)"), 400)
	}
	if err := srv.engine.Console.SetChannelSource(ch, req); err != nil {
		return fail(ctx, err, 400)
	}
	return ok(ctx)
}

func (srv *Server) channelGroupHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[struct {
		Group int `json:"group"`
	}](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	if req.Group < 0 || req.Group > len(srv.engine.Console.Groups) {
		return fail(ctx, serr.New("group out of range"), 400)
	}
	ch.GroupIdx.Store(int32(req.Group))
	return ok(ctx)
}

// ---- modules ----

func (srv *Server) moduleAddHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[struct {
		Name string `json:"name"`
	}](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}

	m, err := module.Create(req.Name)
	if err != nil {
		return fail(ctx, err, 404)
	}
	con := srv.engine.Console
	if err := m.Init(con.SampleRate, con.MaxBlock); err != nil {
		return fail(ctx, serr.Wrap(err, "module", req.Name), 500)
	}
	for _, spec := range m.Params() {
		m.SetParam(spec.ID, spec.Default)
	}
	ch.AddModule(m)
	return ok(ctx)
}

func (srv *Server) moduleRemoveHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[struct {
		Index int `json:"index"`
	}](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	if err := ch.RemoveModule(req.Index); err != nil {
		return fail(ctx, err, 400)
	}
	return ok(ctx)
}

func (srv *Server) moduleParamHandler(ctx rweb.Context) error {
	ch, err := srv.channel(ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	req, err := decodeBody[struct {
		Index int     `json:"index"`
		ID    string  `json:"id"`
		Value float64 `json:"value"`
	}](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	mods := ch.Modules()
	if req.Index < 0 || req.Index >= len(mods) {
		return fail(ctx, serr.New("module index out of range"), 400)
	}
	mods[req.Index].SetParam(req.ID, req.Value)
	return ok(ctx)
}

// ---- group & master ----

func (srv *Server) groupParamHandler(ctx rweb.Context) error {
	idStr := ctx.Request().Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id < 1 || id > len(srv.engine.Console.Groups) {
		return fail(ctx, serr.New("invalid group id", "id", idStr), 400)
	}
	g := srv.engine.Console.Groups[id-1]

	req, err := decodeBody[paramReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	switch req.Name {
	case "gain":
		g.GainDB.Set(clamp(req.Value, -60, 12))
	case "mute":
		g.Mute.Store(req.Value >= 0.5)
	default:
		return fail(ctx, serr.New("unknown group parameter", "param", req.Name), 400)
	}
	return ok(ctx)
}

func (srv *Server) masterParamHandler(ctx rweb.Context) error {
	req, err := decodeBody[paramReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	switch req.Name {
	case "gain":
		srv.engine.Console.Master.GainDB.Set(clamp(req.Value, -60, 12))
	default:
		return fail(ctx, serr.New("unknown master parameter", "param", req.Name), 400)
	}
	return ok(ctx)
}

// ---- scenes (digital memory) ----

type sceneReq struct {
	Name string `json:"name"`
}

func (srv *Server) sceneSaveHandler(ctx rweb.Context) error {
	req, err := decodeBody[sceneReq](ctx)
	if err != nil || req.Name == "" {
		return fail(ctx, serr.New("scene name required"), 400)
	}
	blob, err := json.Marshal(srv.engine.Console.Snapshot())
	if err != nil {
		return fail(ctx, serr.Wrap(err), 500)
	}
	if err := srv.store.Save(req.Name, blob); err != nil {
		return fail(ctx, err, 500)
	}
	logger.Info("Scene saved", "name", req.Name)
	return ok(ctx)
}

func (srv *Server) sceneRecallHandler(ctx rweb.Context) error {
	req, err := decodeBody[sceneReq](ctx)
	if err != nil || req.Name == "" {
		return fail(ctx, serr.New("scene name required"), 400)
	}
	blob, err := srv.store.Load(req.Name)
	if err != nil {
		return fail(ctx, err, 404)
	}
	var scene mixer.SceneState
	if err := json.Unmarshal(blob, &scene); err != nil {
		return fail(ctx, serr.Wrap(err, "scene", req.Name), 500)
	}
	if err := srv.engine.Console.ApplyScene(scene); err != nil {
		return fail(ctx, err, 500)
	}
	logger.Info("Scene recalled", "name", req.Name)
	return ok(ctx)
}

func (srv *Server) sceneDeleteHandler(ctx rweb.Context) error {
	req, err := decodeBody[sceneReq](ctx)
	if err != nil || req.Name == "" {
		return fail(ctx, serr.New("scene name required"), 400)
	}
	if err := srv.store.Delete(req.Name); err != nil {
		return fail(ctx, err, 500)
	}
	return ok(ctx)
}

func (srv *Server) scenesListHandler(ctx rweb.Context) error {
	scenes, err := srv.store.List()
	if err != nil {
		return fail(ctx, err, 500)
	}
	return ctx.WriteJSON(scenes)
}

// ---- recording ----

func (srv *Server) recordStartHandler(ctx rweb.Context) error {
	if _, err := srv.engine.StartRecording(); err != nil {
		return fail(ctx, err, 409)
	}
	return ok(ctx)
}

func (srv *Server) recordStopHandler(ctx rweb.Context) error {
	path, seconds, err := srv.engine.StopRecording()
	if err != nil {
		return fail(ctx, err, 409)
	}
	return ctx.WriteJSON(map[string]any{"path": path, "seconds": seconds})
}

// ---- tutorial progress ----

type lessonPassReq struct {
	Lesson string `json:"lesson"` // lesson name (the stable identity, not catalog index)
	Misses int    `json:"misses"` // wrong-note count for this completed run
}

// tutorialPassHandler records one completed lesson run. Posted by the client
// only at the moment a lesson finishes, so unlike faders there is nothing to
// debounce — each post is a discrete achievement.
func (srv *Server) tutorialPassHandler(ctx rweb.Context) error {
	req, err := decodeBody[lessonPassReq](ctx)
	if err != nil || req.Lesson == "" {
		return fail(ctx, serr.New("lesson name required"), 400)
	}
	if req.Misses < 0 {
		return fail(ctx, serr.New("misses cannot be negative"), 400)
	}
	if err := srv.store.RecordLessonPass(req.Lesson, req.Misses); err != nil {
		return fail(ctx, err, 500)
	}
	logger.Info("Lesson completed", "lesson", req.Lesson, "misses", strconv.Itoa(req.Misses))
	return ok(ctx)
}

func (srv *Server) tutorialProgressHandler(ctx rweb.Context) error {
	list, err := srv.store.ListProgress()
	if err != nil {
		return fail(ctx, err, 500)
	}
	return ctx.WriteJSON(list)
}

// ---- metronome ----

type clickReq struct {
	Accent bool `json:"accent"` // true = bar-start click (higher, louder)
}

// clickHandler fires one metronome click. The browser owns the tempo clock
// (the same setTimeout scheduling family the tutorial's Listen mode uses),
// so like notes this is a discrete event with its own route, not a debounced
// parameter — every beat matters and ordering must hold.
func (srv *Server) clickHandler(ctx rweb.Context) error {
	req, err := decodeBody[clickReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	srv.engine.TriggerClick(req.Accent)
	return ok(ctx)
}

// ---- settings ----

type settingReq struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// settingValidators whitelists the keys the client may persist and validates
// each one's value. A generic key-value endpoint without this gate would let
// any POST grow the settings table unboundedly; with it, adding a new
// persisted preference is one entry here plus a client-side save call.
var settingValidators = map[string]func(value string) error{
	"metro.bpm": func(value string) error {
		bpm, err := strconv.Atoi(value)
		if err != nil {
			return serr.Wrap(err, "msg", "bpm must be an integer")
		}
		// Same range the BPM input and the client-side clamp enforce; the
		// server re-checks because the page renders this value back into the
		// input on every load.
		if bpm < 30 || bpm > 300 {
			return serr.New("bpm out of range", "bpm", value)
		}
		return nil
	},
	"metro.beats": func(value string) error {
		// Exact-set membership (not a numeric range) because the value is
		// rendered back as the selected <option> — anything outside the
		// select's option list would silently select nothing on reload.
		switch value {
		case "2", "3", "4", "6":
			return nil
		}
		return serr.New("beats-per-bar not offered by the meter selector", "beats", value)
	},
}

// settingHandler persists one UI preference. Saves are discrete user actions
// (a committed edit or a finished tap run — the client debounces), so like
// tutorial passes each post simply upserts its row.
func (srv *Server) settingHandler(ctx rweb.Context) error {
	req, err := decodeBody[settingReq](ctx)
	if err != nil {
		return fail(ctx, err, 400)
	}
	validate, known := settingValidators[req.Key]
	if !known {
		return fail(ctx, serr.New("unknown setting key", "key", req.Key), 400)
	}
	if err := validate(req.Value); err != nil {
		return fail(ctx, serr.Wrap(err, "key", req.Key), 400)
	}
	if err := srv.store.SetSetting(req.Key, req.Value); err != nil {
		return fail(ctx, err, 500)
	}
	return ok(ctx)
}

// setting reads one persisted preference for page render, falling back to
// the given default when unset or unreadable — a missing preference must
// never block the mixer page.
func (srv *Server) setting(key, deflt string) string {
	v, found, err := srv.store.GetSetting(key)
	if err != nil {
		logger.LogErr(err, "msg", "reading setting; using default", "key", key)
		return deflt
	}
	if !found {
		return deflt
	}
	return v
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
