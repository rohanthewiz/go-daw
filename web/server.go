// Package web serves the mixer UI and the control API over rweb, and streams
// live meter data to every open browser tab through an SSE hub.
package web

import (
	_ "embed"
	"time"

	"github.com/rohanthewiz/go-daw/audio"
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
			ChannelSize:       32,
			HeartbeatInterval: 15 * time.Second,
		}),
	}

	s := rweb.NewServer(rweb.ServerOptions{Address: cfg.HTTPAddr, Verbose: false})
	s.Use(rweb.RequestInfo)

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
	api.Post("/channel/:id/param", srv.channelParamHandler)
	api.Post("/channel/:id/source", srv.channelSourceHandler)
	api.Post("/channel/:id/source-param", srv.sourceParamHandler)
	api.Post("/channel/:id/note", srv.noteHandler)
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
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()

	for range tick.C {
		con := srv.engine.Console

		payload := struct {
			Ch         []sideLevels `json:"ch"`
			Grp        []sideLevels `json:"grp"`
			Master     sideLevels   `json:"master"`
			Recording  bool         `json:"recording"`
			RecSeconds float64      `json:"recSeconds"`
		}{}

		for _, ch := range con.Channels {
			pl, rl, pr, rr := ch.Meter.Levels()
			payload.Ch = append(payload.Ch, sideLevels{pl, rl, pr, rr})
		}
		for _, g := range con.Groups {
			pl, rl, pr, rr := g.Meter.Levels()
			payload.Grp = append(payload.Grp, sideLevels{pl, rl, pr, rr})
		}
		pl, rl, pr, rr := con.Master.Meter.Levels()
		payload.Master = sideLevels{pl, rl, pr, rr}

		if rec := srv.engine.Recorder(); rec != nil {
			payload.Recording = true
			payload.RecSeconds = rec.Seconds()
		}

		srv.hub.BroadcastAny("meters", payload)
	}
}
