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
	"path/filepath"
	"strings"
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
	// Channel 4 gets a sampled piano when a SoundFont bank is on disk, so the
	// difference between the additive synth (ch 3) and real samples is one
	// piano-channel switch away. Failure just leaves the channel empty — a
	// missing or corrupt bank must not block startup.
	if len(con.Channels) >= 4 {
		if bank := defaultSoundbank(cfg.SoundbanksDir); bank != "" {
			if err := con.SetChannelSource(con.Channels[3], mixer.SourceState{
				Type: "sfont", Path: bank,
			}); err != nil {
				logger.LogErr(err)
			} else {
				con.Channels[3].SetName("SF Piano")
			}
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

// defaultSoundbank picks the startup bank: GeneralUser GS when present (the
// best quality-to-size GM bank, so the best default), otherwise the first
// .sf2 found, otherwise "" (feature stays dormant until a bank is added).
func defaultSoundbank(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "" // no soundbanks folder is a normal, silent case
	}
	first := ""
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".sf2") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(e.Name()), "generaluser") {
			return filepath.Join(dir, e.Name())
		}
		if first == "" {
			first = filepath.Join(dir, e.Name())
		}
	}
	return first
}
