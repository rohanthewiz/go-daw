// go-daw — a web-controlled digital audio workstation / mixing console.
//
//	┌─────────┐   HTTP/SSE   ┌─────────┐  atomics  ┌──────────────┐
//	│ browser │◄────────────►│  web    │◄─────────►│ audio engine │──► CoreAudio
//	│ mixer UI│              │ (rweb)  │           │ (malgo)      │◄── mic/input
//	└─────────┘              └────┬────┘           └──────┬───────┘
//	                              │ scenes JSON           │ master tap
//	                         ┌────▼────┐             ┌────▼─────┐
//	                         │  bytdb  │             │ recorder │──► WAV
//	                         └─────────┘             └──────────┘
//
// main is wiring-only: every subsystem is constructed here in dependency
// order, and shutdown unwinds the same order in reverse.
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/rohanthewiz/go-daw/audio"
	"github.com/rohanthewiz/go-daw/config"
	"github.com/rohanthewiz/go-daw/mixer"
	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/go-daw/module/builtin"
	"github.com/rohanthewiz/go-daw/store"
	"github.com/rohanthewiz/go-daw/web"
	"github.com/rohanthewiz/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		logger.LogErr(err, "msg", "loading configuration")
		os.Exit(1)
	}

	// Scene store first — a broken database is worth knowing about before
	// the audio device grabs the hardware.
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.LogErr(err, "msg", "opening scene store")
		os.Exit(1)
	}

	// Modules: builtins registered explicitly (no init() magic — the list
	// below is the single place to see what ships in the binary), then any
	// external .so plugins. Plugin failures only log; they never block startup.
	module.Register("tremolo", builtin.NewTremolo)
	module.Register("delay", builtin.NewDelay)
	module.LoadPlugins(cfg.PluginsDir)

	eng := audio.NewEngine(cfg)

	// Default sources so a fresh launch makes sound and shows live input:
	// channel 1 gets a quiet test oscillator, channel 2 the live feed (when
	// capture is available), channel 3 a polyphonic synth so the virtual
	// piano is playable immediately. Everything is changeable from the UI.
	con := eng.Console
	if len(con.Channels) >= 1 {
		if err := con.SetChannelSource(con.Channels[0], mixer.SourceState{
			Type: "osc", FreqHz: 220, LevelDB: -24,
		}); err != nil {
			logger.LogErr(err)
		}
	}
	if len(con.Channels) >= 3 {
		if err := con.SetChannelSource(con.Channels[2], mixer.SourceState{Type: "synth"}); err != nil {
			logger.LogErr(err)
		} else {
			con.Channels[2].SetName("Piano")
		}
	}

	if err := eng.Start(); err != nil {
		logger.LogErr(err, "msg", "starting audio engine")
		st.Close()
		os.Exit(1)
	}

	if eng.Duplex && len(con.Channels) >= 2 {
		if err := con.SetChannelSource(con.Channels[1], mixer.SourceState{Type: "live"}); err != nil {
			logger.LogErr(err)
		}
	}

	// Graceful shutdown: stop the device (finalizing any recording) before
	// closing the store, mirroring construction order in reverse.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		logger.Info("Shutting down")
		eng.Stop()
		if err := st.Close(); err != nil {
			logger.LogErr(err)
		}
		os.Exit(0)
	}()

	// Blocks serving HTTP for the life of the process.
	if err := web.Start(cfg, eng, st); err != nil {
		logger.LogErr(err, "msg", "web server exited")
		eng.Stop()
		st.Close()
		os.Exit(1)
	}
}
