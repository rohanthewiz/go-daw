package mixer

import (
	"github.com/rohanthewiz/go-daw/dsp"
	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/serr"
)

// SetModuleParam writes one insert parameter after validating it against the
// module's own declared ParamSpec range. Control plane; every path that sets
// a module parameter from outside data (HTTP, scene recall) goes through
// here, so the validation lives in one place instead of in every plugin.
//
// Why the host clamps rather than trusting each module: plugins are written
// against the UI's slider ranges and reasonably assume they hold. Unclamped,
// one request is enough to break the engine, not just a sound:
//
//   - flanger depth=1e20: its delay-index wrap loop adds the buffer length
//     to a number too large to change, so it never terminates — on the
//     CoreAudio thread, freezing all audio;
//   - bass_xpander drive=0: tanh(0)/0 = NaN into the strip;
//   - delay feedback=2: the echo grows without bound to Inf.
//
// Unknown parameter IDs are rejected rather than forwarded, so a stale tab
// or a typo gets an error instead of a silent no-op.
func SetModuleParam(m module.AudioModule, id string, value float64) error {
	if !dsp.Finite(value) {
		return serr.New("module parameter must be a finite number", "module", m.Name(), "param", id)
	}
	for _, spec := range m.Params() {
		if spec.ID != id {
			continue
		}
		lo, hi := spec.Min, spec.Max
		if lo > hi { // tolerate a spec written max-first
			lo, hi = hi, lo
		}
		m.SetParam(id, clampF(value, lo, hi))
		return nil
	}
	return serr.New("unknown module parameter", "module", m.Name(), "param", id)
}
