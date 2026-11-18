// Package web serves the mixer UI and the control API over rweb, and streams
// live meter data to every open browser tab through an SSE hub.
package web

import (
	_ "embed"
	"strconv"
	"time"

	"github.com/rohanthewiz/go-daw/audio"
	"github.com/rohanthewiz/go-daw/audio/source"
	"github.com/rohanthewiz/go-daw/config"
	"github.com/rohanthewiz/go-daw/store"
	styl "github.com/rohanthewiz/go-styl"
	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/rweb"
	"github.com/rohanthewiz/serr"
)

//go:embed styles/main.styl
var stylSource string

//go:embed assets/app.js
var appJS string

// Server bundles the dependencies every handler needs.
type Server struct {
	cfg    *config.Config
	engine *audio.Engine
	store  *store.Store
	hub    *rweb.SSEHub
	css    string // compiled once at startup
}

// Start compiles the stylesheet, wires all routes, launches the meter
// broadcast loop, and blocks serving HTTP.
func Start(cfg *config.Config, engine *audio.Engine, st *store.Store) error {
	// Compile the Stylus source once, up front. Failing fast here beats
	// discovering a stylesheet typo on the first browser request.
	css, err := styl.Compile(stylSource, styl.Options{})
	if err != nil {
		return serr.Wrap(err, "msg", "compiling main.styl")
	}

	srv := &Server{
		cfg:    cfg,
		engine: engine,
		store:  st,
		css:    css,
		hub: rweb.NewSSEHub(rweb.SSEHubOptions{
			ChannelSize: 32,
			// Passing an options struct leaves MaxDropped at its zero value,
			// which *disables* eviction (the documented default of 3 only
			// applies without options). A stalled tab would then stay
			// registered forever. 25 consecutive drops ≈ 2 s of meter frames
			// on top of the 32-frame buffer: only a truly stuck client trips it.
			MaxDropped:        25,
			HeartbeatInterval: 15 * time.Second,
		}),
	}

	s := rweb.NewServer(rweb.ServerOptions{Address: cfg.HTTPAddr, Verbose: false})
	// recoverPanics first so it wraps everything after it, including the
	// origin check and request logging.
	s.Use(recoverPanics, sameOriginWrites, rweb.RequestInfo)

	// Pages & assets
	s.Get("/", srv.pageHandler)
	s.Get("/styles.css", func(ctx rweb.Context) error { return rweb.CSS(ctx, srv.css) })
	s.Get("/assets/app.js", func(ctx rweb.Context) error { return rweb.JS(ctx, appJS) })
	s.Get("/events", srv.hub.Handler(s))

	// Control API
	api := s.Group("/api")
	api.Get("/state", srv.stateHandler)
	api.Get("/modules", srv.modulesHandler)
	api.Get("/scenes", srv.scenesListHandler)
	api.Get("/lessons", srv.lessonsHandler)
	api.Get("/tour", srv.tourHandler)
	api.Get("/tutorial/progress", srv.tutorialProgressHandler)
	api.Post("/tutorial/pass", srv.tutorialPassHandler)
	api.Get("/soundfonts", srv.soundfontsHandler)
	api.Get("/midifiles", srv.midiFilesHandler)
	api.Post("/channel/:id/param", srv.channelParamHandler)
	api.Post("/channel/:id/source", srv.channelSourceHandler)
	api.Post("/channel/:id/source-param", srv.sourceParamHandler)
	api.Post("/channel/:id/note", srv.noteHandler)
	api.Post("/channel/:id/pedal", srv.pedalHandler)
	api.Post("/channel/:id/midi", srv.midiControlHandler)
	api.Post("/channel/:id/group", srv.channelGroupHandler)
	api.Post("/channel/:id/module/add", srv.moduleAddHandler)
	api.Post("/channel/:id/module/remove", srv.moduleRemoveHandler)
	api.Post("/channel/:id/module/param", srv.moduleParamHandler)
	api.Post("/group/:id/param", srv.groupParamHandler)
	api.Post("/master/param", srv.masterParamHandler)
	api.Post("/scene/save", srv.sceneSaveHandler)
	api.Post("/scene/recall", srv.sceneRecallHandler)
	api.Post("/scene/delete", srv.sceneDeleteHandler)
	api.Post("/record/start", srv.recordStartHandler)
	api.Post("/record/stop", srv.recordStopHandler)
	api.Post("/click", srv.clickHandler)
	api.Post("/setting", srv.settingHandler)

	go srv.meterLoop()

	logger.Info("Web UI listening", "addr", cfg.HTTPAddr)
	return s.Run()
}

// meterLoop broadcasts console levels ~12.5 times per second — fast enough
// for lively meters, slow enough to be negligible load. Reading the packed
// meter atomics is wait-free, so this loop never disturbs the audio thread.
func (srv *Server) meterLoop() {
	type sideLevels struct {
		PL float32 `json:"pl"`
		RL float32 `json:"rl"`
		PR float32 `json:"pr"`
		RR float32 `json:"rr"`
	}
	// midiInfo rides the meter broadcast so every open tab sees transport
	// state/position without polling — the meter cadence (~12.5 Hz) is already
	// exactly right for a position readout.
	type midiInfo struct {
		State string  `json:"state"`
		Pos   float64 `json:"pos"`
		Len   float64 `json:"len"`
	}
	// levels builds one meter entry with every value forced finite.
	// encoding/json refuses NaN/±Inf, and one bad float fails the whole
	// frame, freezing meters, record clock and MIDI readouts in every tab.
	// The strips contain their own faults, so this should never trigger; it
	// keeps the UI stream alive even if something upstream slips through.
	levels := func(pl, rl, pr, rr float32) sideLevels {
		fin := func(v float32) float32 {
			if v-v != 0 {
				return 0
			}
			return v
		}
		return sideLevels{fin(pl), fin(rl), fin(pr), fin(rr)}
	}

	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	con := srv.engine.Console
	// lastFaults/faultsRising implement edge-triggered fault logging: a
	// plugin that emits NaN every block would otherwise log 12 lines a
	// second. One line when a strip starts faulting; it re-arms after a
	// quiet tick.
	lastFaults := make([]uint64, len(con.Channels))
	faultsRising := make([]bool, len(con.Channels))

	for range tick.C {
		anyFaults := false
		for i, ch := range con.Channels {
			f := ch.Faults()
			rising := f > lastFaults[i]
			if rising && !faultsRising[i] {
				logger.LogErr(serr.New("channel produced non-finite audio or a module panicked; strip silenced and reset",
					"channel", strconv.Itoa(ch.ID), "faultBlocks", strconv.FormatUint(f, 10)))
			}
			faultsRising[i] = rising
			lastFaults[i] = f
			anyFaults = anyFaults || f > 0
		}

		// No tab is listening: skip building and encoding a frame nobody
		// will read. (The fault check above still runs, so logs stay live.)
		if srv.hub.ClientCount() == 0 {
			continue
		}

		payload := struct {
			Ch         []sideLevels `json:"ch"`
			Grp        []sideLevels `json:"grp"`
			Master     sideLevels   `json:"master"`
			Midi       []*midiInfo  `json:"midi,omitempty"`
			Recording  bool         `json:"recording"`
			RecSeconds float64      `json:"recSeconds"`
			// RecDropped counts blocks the take lost because the disk fell
			// behind; RecError is set once a write has failed and nothing
			// more is reaching the file. Both omitted while healthy.
			RecDropped uint64 `json:"recDropped,omitempty"`
			RecError   string `json:"recError,omitempty"`
			// Faults is per-channel (aligned with Ch): blocks silenced by
			// the strip's non-finite guard or a panicking insert. Omitted
			// entirely while every strip is clean, like Midi.
			Faults       []uint64 `json:"faults,omitempty"`
			MasterFaults uint64   `json:"masterFaults,omitempty"`
		}{}

		if anyFaults {
			payload.Faults = append([]uint64(nil), lastFaults...)
		}
		payload.MasterFaults = con.Master.Faults()

		anyMidi := false
		for _, ch := range con.Channels {
			payload.Ch = append(payload.Ch, levels(ch.Meter.Levels()))

			// Positionally aligned with Ch; nil for channels without a player,
			// so the JSON stays tiny when the feature is idle.
			var mi *midiInfo
			if mp, isMidi := ch.Source().(*source.MidiPlayer); isMidi {
				mi = &midiInfo{State: mp.PlayState(), Pos: mp.PosSeconds(), Len: mp.LengthSeconds()}
				anyMidi = true
			}
			payload.Midi = append(payload.Midi, mi)
		}
		if !anyMidi {
			payload.Midi = nil
		}
		for _, g := range con.Groups {
			payload.Grp = append(payload.Grp, levels(g.Meter.Levels()))
		}
		payload.Master = levels(con.Master.Meter.Levels())

		if rec := srv.engine.Recorder(); rec != nil {
			payload.Recording = true
			payload.RecSeconds = rec.Seconds()
			payload.RecDropped = rec.Dropped()
			if err := rec.Err(); err != nil {
				payload.RecError = err.Error()
			}
		}

		srv.hub.BroadcastAny("meters", payload)
	}
}
