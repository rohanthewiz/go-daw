package record

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/rohanthewiz/logger"
	"github.com/rohanthewiz/serr"
)

// Recorder bounces the master bus to a WAV file.
//
//	audio callback ──Push──► F32Ring ──Pop──► drain goroutine ──► bufio ──► disk
//
// The ring is the load-bearing piece: it decouples the realtime thread
// (which can never wait on disk) from file I/O (which occasionally stalls
// for tens of milliseconds). ~1.3s of ring slack absorbs those stalls.
type Recorder struct {
	ring    *f32Ring
	done    chan struct{} // closed when the drain goroutine has fully flushed
	stop    atomic.Bool
	path    string
	frames  atomic.Uint64
	started time.Time
	rate    int
}

// Start creates recordings/bounce-<timestamp>.wav and launches the drain
// goroutine. The returned Recorder is ready to receive Push immediately.
func Start(dir string, sampleRate int) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, serr.Wrap(err, "dir", dir)
	}
	path := filepath.Join(dir, "bounce-"+time.Now().Format("20060102-150405")+".wav")

	w, err := newWavWriter(path, sampleRate)
	if err != nil {
		return nil, serr.Wrap(err)
	}

	rec := &Recorder{
		ring:    newF32Ring(1 << 18),
		done:    make(chan struct{}),
		path:    path,
		started: time.Now(),
		rate:    sampleRate,
	}

	go rec.drain(w)
	logger.Info("Recording started", "path", path)
	return rec, nil
}

// Push feeds interleaved master-bus samples. Audio thread; wait-free.
func (rec *Recorder) Push(interleaved []float32) {
	rec.ring.Push(interleaved)
	rec.frames.Add(uint64(len(interleaved) / 2))
}

// drain pumps ring -> disk until stopped, then flushes the remainder.
func (rec *Recorder) drain(w *wavWriter) {
	defer close(rec.done)
	buf := make([]float32, 8192)

	flush := func() bool {
		for {
			n := rec.ring.Pop(buf)
			if n == 0 {
				return true
			}
			if err := w.writeSamples(buf[:n]); err != nil {
				logger.LogErr(err, "msg", "recording write failed; stopping drain")
				return false
			}
		}
	}

	for !rec.stop.Load() {
		if !flush() {
			w.close()
			return
		}
		// 20ms poll ≈ 1920 frames at 48k — far inside the ring's slack.
		time.Sleep(20 * time.Millisecond)
	}
	flush() // final drain after the producer has been detached

	if err := w.close(); err != nil {
		logger.LogErr(err, "msg", "closing WAV file")
	}
	if o := rec.ring.Overruns(); o > 0 {
		logger.Info("Recording had dropped blocks (disk could not keep up)", "overruns", int(o))
	}
}

// Stop ends the recording and blocks until the file is fully written.
// The caller must detach the recorder from the engine BEFORE calling Stop
// (swap the atomic pointer to nil) so no Push races the final flush.
func (rec *Recorder) Stop() (path string, seconds float64) {
	rec.stop.Store(true)
	<-rec.done
	logger.Info("Recording stopped", "path", rec.path)
	return rec.path, float64(rec.frames.Load()) / float64(rec.rate)
}

// Path returns the output file path.
func (rec *Recorder) Path() string { return rec.path }

// Seconds returns the elapsed recorded duration so far.
func (rec *Recorder) Seconds() float64 {
	return float64(rec.frames.Load()) / float64(rec.rate)
}
