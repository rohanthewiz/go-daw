// Smoke test for recording: plays a tone, records ~1.5s of the master bus,
// and verifies the WAV file exists with a plausible size and duration.
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
		ChannelCount: 1, GroupCount: 0,
		SampleRate: 48000, BlockSize: 256,
		Duplex: false, RecordingsDir: "test_scripts/record_smoke/out",
	}

	eng := audio.NewEngine(cfg)
	ch := eng.Console.Channels[0]
	if err := eng.Console.SetChannelSource(ch, mixer.SourceState{Type: "osc", FreqHz: 440, LevelDB: -20}); err != nil {
		panic(err)
	}
	if err := eng.Start(); err != nil {
		panic(err)
	}
	defer eng.Stop()

	if _, err := eng.StartRecording(); err != nil {
		fmt.Println("start recording failed:", err)
		os.Exit(1)
	}
	time.Sleep(1500 * time.Millisecond)
	path, seconds, err := eng.StopRecording()
	if err != nil {
		fmt.Println("stop recording failed:", err)
		os.Exit(1)
	}

	info, err := os.Stat(path)
	if err != nil {
		fmt.Println("recorded file missing:", err)
		os.Exit(1)
	}
	fmt.Printf("recorded %s: %.2fs, %d bytes\n", path, seconds, info.Size())

	// 1.5s stereo 16-bit 48k ≈ 288000 bytes + 44 header; accept a generous range.
	if seconds < 1.0 || info.Size() < 150000 {
		fmt.Println("FAIL: recording too short/small")
		os.Exit(1)
	}
	fmt.Println("OK: recording pipeline working")
}
