package module

import (
	"sort"
	"sync"

	"github.com/rohanthewiz/serr"
)

// The registry maps module names to factories. Factories (not instances)
// are registered because each channel insert needs its own state — sharing
// one delay buffer across channels would smear their audio together.
//
// A plain mutex is fine here: the registry is only touched from the control
// plane (startup registration, plugin loading, HTTP "add module" requests) —
// never from the audio thread.
var (
	regMu    sync.Mutex
	registry = map[string]func() AudioModule{}
)

// Register adds a factory under its module name. Later registrations of the
// same name win, which lets a .so plugin override a builtin intentionally.
func Register(name string, factory func() AudioModule) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[name] = factory
}

// Create instantiates a fresh, un-Init'ed module by name.
func Create(name string) (AudioModule, error) {
	regMu.Lock()
	factory, ok := registry[name]
	regMu.Unlock()
	if !ok {
		return nil, serr.New("unknown module", "name", name)
	}
	return factory(), nil
}

// Available lists registered module names, sorted for stable UI dropdowns.
func Available() []string {
	regMu.Lock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	regMu.Unlock()
	sort.Strings(names)
	return names
}
