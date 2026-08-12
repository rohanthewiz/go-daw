// styl_check compiles web/styles/main.styl and prints the resulting CSS (or the
// compile error). Kept as a script so stylesheet edits can be validated without
// booting the audio engine, which needs a device and microphone permission.
package main

import (
	"fmt"
	"os"

	styl "github.com/rohanthewiz/go-styl"
)

func main() {
	src, err := os.ReadFile("web/styles/main.styl")
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	css, err := styl.Compile(string(src), styl.Options{})
	if err != nil {
		fmt.Println("COMPILE ERROR:", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "-print" {
		fmt.Println(css)
		return
	}
	fmt.Printf("ok: %d bytes of CSS\n", len(css))
}
