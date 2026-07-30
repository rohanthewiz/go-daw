package web

import (
	"encoding/json"
	"strconv"

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
		Console:   srv.engine.Console,
		Scenes:    scenes,
		Recording: srv.engine.Recorder() != nil,
		Duplex:    srv.engine.Duplex,
	})
	return ctx.WriteHTML(html)
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
	default:
		return fail(ctx, serr.New("channel source has no adjustable parameters"), 400)
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
	syn, isSynth := ch.Source().(*source.PolySynth)
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

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
