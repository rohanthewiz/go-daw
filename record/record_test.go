package record

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// wavSizes reads the two header size fields straight off disk — what any
// player sees, whether or not the writer ever closed the file.
func wavSizes(t *testing.T, path string) (riff, data uint32, fileLen int64) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < wavHeaderSize || string(b[0:4]) != "RIFF" || string(b[36:40]) != "data" {
		t.Fatalf("not a WAV header: % x", b[:min(len(b), wavHeaderSize)])
	}
	return binary.LittleEndian.Uint32(b[4:8]), binary.LittleEndian.Uint32(b[40:44]), int64(len(b))
}

func TestRingPushPopAndOverrun(t *testing.T) {
	rb := newF32Ring(8) // already a power of two
	if !rb.Push([]float32{1, 2, 3, 4, 5, 6}) {
		t.Fatal("push into empty ring refused")
	}
	if rb.Push([]float32{7, 8, 9}) {
		t.Fatal("push past capacity accepted (6+3 > 8)")
	}
	if rb.Overruns() != 1 {
		t.Fatalf("overruns = %d, want 1", rb.Overruns())
	}
	out := make([]float32, 4)
	if n := rb.Pop(out); n != 4 || out[0] != 1 || out[3] != 4 {
		t.Fatalf("pop = %d %v", n, out)
	}
	// Wraps around the mask: 2 left + 5 new = 7 ≤ 8.
	if !rb.Push([]float32{7, 8, 9, 10, 11}) {
		t.Fatal("push after pop refused")
	}
	out = make([]float32, 16)
	n := rb.Pop(out)
	want := []float32{5, 6, 7, 8, 9, 10, 11}
	if n != len(want) {
		t.Fatalf("pop = %d, want %d", n, len(want))
	}
	for i, v := range want {
		if out[i] != v {
			t.Fatalf("out[%d] = %v, want %v (wraparound order)", i, out[i], v)
		}
	}
}

// TestWavCheckpointSurvivesCrash: after a checkpoint the file on disk must
// already be a complete WAV describing every sample written — the state a
// crash would leave behind, since close never runs.
func TestWavCheckpointSurvivesCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "take.wav")
	w, err := newWavWriter(path, 48000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.f.Close() // no w.close(): simulate the process dying

	if err := w.writeSamples(make([]float32, 9600)); err != nil {
		t.Fatal(err)
	}
	if err := w.checkpoint(); err != nil {
		t.Fatal(err)
	}
	riff, data, n := wavSizes(t, path)
	if data != 9600*2 || riff != 36+data || n != wavHeaderSize+int64(data) {
		t.Fatalf("after checkpoint: riff=%d data=%d file=%d, want data=%d", riff, data, n, 9600*2)
	}

	// Writing continues at the end (pwrite did not move the offset), and the
	// next checkpoint grows the sizes to match.
	if err := w.writeSamples(make([]float32, 400)); err != nil {
		t.Fatal(err)
	}
	if err := w.checkpoint(); err != nil {
		t.Fatal(err)
	}
	if _, data, n = wavSizes(t, path); data != 10000*2 || n != wavHeaderSize+int64(data) {
		t.Fatalf("second checkpoint: data=%d file=%d", data, n)
	}
}

func TestWavSampleConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conv.wav")
	w, err := newWavWriter(path, 48000)
	if err != nil {
		t.Fatal(err)
	}
	in := []float32{0, 1, -1, 2, -2, 0.5, float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))}
	want := []int16{0, 32767, -32767, 32767, -32767, 16383, 0, 32767, -32767}
	if err := w.writeSamples(in); err != nil {
		t.Fatal(err)
	}
	if err := w.close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for i, v := range want {
		got := int16(binary.LittleEndian.Uint16(b[wavHeaderSize+i*2:]))
		if got != v {
			t.Errorf("sample %d (%v): got %d, want %d", i, in[i], got, v)
		}
	}
}

// TestWavRefusesSizeOverflow: past ~4 GiB the uint32 size fields would wrap
// and the take would read back as a few seconds long. The writer refuses
// instead (the drain then finalizes a valid file).
func TestWavRefusesSizeOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.wav")
	w, err := newWavWriter(path, 48000)
	if err != nil {
		t.Fatal(err)
	}
	defer w.f.Close()
	w.samples = maxWavDataBytes/2 - 1 // pretend ~6 h are already on disk
	if err := w.writeSamples([]float32{0, 0}); err != errWavFull {
		t.Fatalf("got %v, want errWavFull", err)
	}
}

// TestRecorderEndToEnd drives the real drain goroutine: what the audio thread
// pushes must all land in a valid WAV, and Seconds must match the frames.
func TestRecorderEndToEnd(t *testing.T) {
	rec, err := Start(t.TempDir(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	block := make([]float32, 256*2)
	for i := range block {
		block[i] = 0.25
	}
	const blocks = 200 // 51200 frames ≈ 1.07 s
	for range blocks {
		rec.Push(block)
		time.Sleep(50 * time.Microsecond) // let the drain keep pace, as a real callback would
	}
	path, seconds := rec.Stop()
	if rec.Err() != nil {
		t.Fatalf("drain error: %v", rec.Err())
	}
	if rec.Dropped() != 0 {
		t.Fatalf("dropped %d blocks in an unloaded test", rec.Dropped())
	}
	frames := blocks * 256
	if want := float64(frames) / 48000; math.Abs(seconds-want) > 1e-9 {
		t.Fatalf("seconds = %v, want %v", seconds, want)
	}
	_, data, n := wavSizes(t, path)
	if data != uint32(frames*4) || n != wavHeaderSize+int64(data) {
		t.Fatalf("data=%d file=%d, want data=%d", data, n, frames*4)
	}
}

// TestBackToBackTakesNeverOverwrite: two takes started within the same
// second used to share a filename, and os.Create truncated the first.
func TestBackToBackTakesNeverOverwrite(t *testing.T) {
	dir := t.TempDir()
	first, err := Start(dir, 48000)
	if err != nil {
		t.Fatal(err)
	}
	first.Push(make([]float32, 512))
	firstPath, _ := first.Stop()

	second, err := Start(dir, 48000)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, _ := second.Stop()

	if firstPath == secondPath {
		t.Fatalf("both takes wrote %s", firstPath)
	}
	if _, data, _ := wavSizes(t, firstPath); data != 512*2 {
		t.Fatalf("first take was clobbered: data=%d, want %d", data, 512*2)
	}
}
