package module

import (
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
		p, err := plugin.Open(path)
		if err != nil {
			logger.LogErr(serr.Wrap(err, "path", path), "msg", "skipping plugin (open failed — likely built with a different toolchain or dep versions)")
			continue
		}
		sym, err := p.Lookup("NewModule")
		if err != nil {
			logger.LogErr(serr.Wrap(err, "path", path), "msg", "skipping plugin (no NewModule export)")
			continue
		}
		factory, ok := sym.(func() AudioModule)
		if !ok {
			logger.LogErr(serr.New("NewModule has wrong signature", "path", path))
			continue
		}

		// Probe one instance for its name, then register the factory itself.
		name := factory().Name()
		Register(name, factory)
		loaded = append(loaded, name)
		logger.Info("Loaded plugin module", "name", name, "path", path)
	}
	return loaded
}
