// Smoke test for the audio engine: plays a 220Hz sine on channel 1 for two
// seconds through the full DSP chain (playback-only, no capture). Success is
// hearing a clean tone and seeing non-zero meter values printed.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rohanthewiz/go-daw/audio"
	"github.com/rohanthewiz/go-daw/config"
	"github.com/rohanthewiz/go-daw/mixer"
)

func main() {
	cfg := &config.Config{
		ChannelCount: 2, GroupCount: 1,
		SampleRate: 48000, BlockSize: 256,
		Duplex: false, RecordingsDir: "recordings",
	}

	eng := audio.NewEngine(cfg)
	ch := eng.Console.Channels[0]
	if err := eng.Console.SetChannelSource(ch, mixer.SourceState{Type: "osc", FreqHz: 220, LevelDB: -18}); err != nil {
		fmt.Println("source error:", err)
		os.Exit(1)
	}

	if err := eng.Start(); err != nil {
		fmt.Println("engine start error:", err)
		os.Exit(1)
	}

	time.Sleep(2 * time.Second)

	pl, rl, _, _ := ch.Meter.Levels()
	mpl, mrl, _, _ := eng.Console.Master.Meter.Levels()
	eng.Stop()

	fmt.Printf("channel meter peak=%.4f rms=%.4f\n", pl, rl)
	fmt.Printf("master  meter peak=%.4f rms=%.4f\n", mpl, mrl)
	if mpl < 0.01 {
		fmt.Println("FAIL: no signal reached the master bus")
		os.Exit(1)
	}
	fmt.Println("OK: tone rendered through the full chain")
}
