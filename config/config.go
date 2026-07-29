// Package config holds all tunable knobs for the DAW: engine geometry
// (channels, groups, sample rate, block size), audio I/O mode, HTTP address,
// and filesystem locations. Resolution order is: compiled defaults -> JSON
// config file overlay -> command-line flag overrides. Flags win because they
// are the most deliberate, session-scoped choice a user can make.
package config

import (
	"encoding/json"
	"flag"
	"os"

	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// Config is the full application configuration.
// JSON tags match the config.json overlay file.
type Config struct {
	ChannelCount  int    `json:"channels"`      // number of mixer input channels
	GroupCount    int    `json:"groups"`        // number of group buses (channels route to a group or direct to master)
	SampleRate    int    `json:"sampleRate"`    // engine rate in Hz; sources are resampled to this at load
	BlockSize     int    `json:"blockSize"`     // preferred period size in frames (~5.3ms at 256/48k)
	Duplex        bool   `json:"duplex"`        // true = capture + playback; false = playback only
	HTTPAddr      string `json:"httpAddr"`      // web UI listen address
	DBPath        string `json:"dbPath"`        // bytdb scene store file
	RecordingsDir string `json:"recordingsDir"` // bounced WAVs land here
	PluginsDir    string `json:"pluginsDir"`    // *.so audio modules are loaded from here
}

// defaults returns the baseline config. 48kHz/256-frame blocks is the sweet
// spot on CoreAudio: low enough latency for live monitoring, large enough to
// keep the per-block DSP overhead negligible.
func defaults() Config {
	return Config{
		ChannelCount:  8,
		GroupCount:    4,
		SampleRate:    48000,
		BlockSize:     256,
		Duplex:        true,
		HTTPAddr:      ":8000",
		DBPath:        "godaw.db",
		RecordingsDir: "recordings",
		PluginsDir:    "plugins",
	}
}

// Load builds the effective config. It parses flags itself so main stays
// wiring-only. A missing config file is not an error (defaults are complete);
// a malformed one is, because silently ignoring a typo'd config is worse than
// failing fast.
func Load() (*Config, error) {
	cfg := defaults()

	// Flags are declared against temporary holders so we can distinguish
	// "flag not given" (keep file/default value) from "flag given".
	configPath := flag.String("config", "config.json", "path to JSON config file")
	channels := flag.Int("channels", 0, "number of mixer channels (overrides config)")
	addr := flag.String("addr", "", "HTTP listen address (overrides config)")
	sampleRate := flag.Int("sr", 0, "sample rate in Hz (overrides config)")
	blockSize := flag.Int("block", 0, "block size in frames (overrides config)")
	noInput := flag.Bool("no-input", false, "disable audio capture (playback only)")
	flag.Parse()

	if data, err := os.ReadFile(*configPath); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, serr.Wrap(err, "file", *configPath, "msg", "config file is not valid JSON")
		}
		logger.Info("Loaded config file", "path", *configPath)
	} else {
		logger.Info("No config file found, using defaults", "path", *configPath)
	}

	// Zero-valued flags mean "not provided" — safe because none of these
	// have a meaningful zero (0 channels, empty addr, etc.).
	if *channels > 0 {
		cfg.ChannelCount = *channels
	}
	if *addr != "" {
		cfg.HTTPAddr = *addr
	}
	if *sampleRate > 0 {
		cfg.SampleRate = *sampleRate
	}
	if *blockSize > 0 {
		cfg.BlockSize = *blockSize
	}
	if *noInput {
		cfg.Duplex = false
	}

	if cfg.ChannelCount < 1 || cfg.ChannelCount > 64 {
		return nil, serr.New("channel count out of range 1..64")
	}
	if cfg.GroupCount < 0 || cfg.GroupCount > 16 {
		return nil, serr.New("group count out of range 0..16")
	}

	return &cfg, nil
}
