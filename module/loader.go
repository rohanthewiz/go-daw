package module

import (
	"fmt"
	"path/filepath"
	"plugin"

	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// LoadPlugins opens every *.so in dir and registers the module each exports.
// The contract for a plugin is a package main that exports:
//
//	func NewModule() module.AudioModule
//
// Failures are logged per file and skipped — a bad or stale .so must never
// prevent the DAW from starting, because the builtin modules and the whole
// mixer are independent of it. (Note: Go plugins demand the .so be built
// with the identical toolchain and dependency versions as this binary; see
// the README for the exact build command.)
func LoadPlugins(dir string) (loaded []string) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.so"))
	if err != nil {
		logger.LogErr(serr.Wrap(err, "dir", dir), "msg", "plugin glob failed")
		return nil
	}

	for _, path := range paths {
		if name, ok := loadPlugin(path); ok {
			loaded = append(loaded, name)
		}
	}
	return loaded
}

// loadPlugin opens one .so and registers its factory, reporting the module
// name on success. Split out of LoadPlugins so a deferred recover can wrap
// exactly one plugin: plugin.Open runs the plugin's package init code, and
// probing calls its NewModule and Name — all third-party code that can
// panic. Without the recover, one broken .so would abort startup, which
// contradicts this package's promise that a bad plugin is logged and
// skipped.
func loadPlugin(path string) (name string, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logger.LogErr(serr.New("plugin panicked while loading", "path", path, "panic", fmt.Sprint(r)),
				"msg", "skipping plugin")
			name, ok = "", false
		}
	}()

	p, err := plugin.Open(path)
	if err != nil {
		logger.LogErr(serr.Wrap(err, "path", path), "msg", "skipping plugin (open failed — likely built with a different toolchain or dep versions)")
		return "", false
	}
	sym, err := p.Lookup("NewModule")
	if err != nil {
		logger.LogErr(serr.Wrap(err, "path", path), "msg", "skipping plugin (no NewModule export)")
		return "", false
	}
	factory, isFactory := sym.(func() AudioModule)
	if !isFactory {
		logger.LogErr(serr.New("NewModule has wrong signature", "path", path))
		return "", false
	}

	// Probe one instance for its name, then register the factory itself. A
	// nil instance (an interface holding nil) would otherwise surface later
	// as a nil-pointer panic inside an HTTP handler or the audio thread.
	probe := factory()
	if probe == nil {
		logger.LogErr(serr.New("NewModule returned nil", "path", path), "msg", "skipping plugin")
		return "", false
	}
	name = probe.Name()
	if name == "" {
		logger.LogErr(serr.New("plugin module has an empty name", "path", path), "msg", "skipping plugin")
		return "", false
	}
	Register(name, factory)
	logger.Info("Loaded plugin module", "name", name, "path", path)
	return name, true
}
