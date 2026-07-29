// Smoke test for the plugin system: registers builtins, loads plugins/*.so,
// and runs one block through each module. Validates the Go plugin toolchain
// identity on this machine (Risk B).
package main

import (
	"fmt"
	"os"

	"github.com/rohanthewiz/go-daw/module"
	"github.com/rohanthewiz/go-daw/module/builtin"
)

func main() {
	module.Register("tremolo", builtin.NewTremolo)
	module.Register("delay", builtin.NewDelay)
	loaded := module.LoadPlugins("plugins")
	fmt.Println("plugins loaded:", loaded)
	fmt.Println("available:", module.Available())

	l := make([]float32, 256)
	r := make([]float32, 256)
	for _, name := range module.Available() {
		m, err := module.Create(name)
		if err != nil {
			fmt.Println("create failed:", err)
			os.Exit(1)
		}
		if err := m.Init(48000, 4096); err != nil {
			fmt.Println("init failed:", err)
			os.Exit(1)
		}
		l[0], r[0] = 1, 1
		m.Process(l, r)
		fmt.Printf("module %-8s ok (%d params)\n", name, len(m.Params()))
	}

	if len(loaded) == 0 {
		fmt.Println("FAIL: flanger.so did not load")
		os.Exit(1)
	}
	fmt.Println("OK: plugin system fully working")
}
