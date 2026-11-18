package record

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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
	frames  atomic.Uint64 // frames accepted into the ring (dropped blocks excluded)
	started time.Time
	rate    int

	// failErr holds the write error that ended the drain early (disk full,
	// volume unplugged, WAV size limit). nil while healthy. The engine still
	// holds the recorder in that state, so without this the UI would keep
	// showing a running take that is no longer reaching disk.
	failErr atomic.Pointer[error]
}

// maxTakeSuffix bounds the collision search in Start. Reaching it would
// take 99 takes started inside one second.
const maxTakeSuffix = 99

// checkpointEvery is how many 20 ms drain polls pass between header
// checkpoints (~1 s): often enough that a crash loses at most a second of
// the take, rare enough that the extra flush + two pwrites cost nothing.
const checkpointEvery = 50

// Start creates recordings/bounce-<timestamp>.wav and launches the drain
// goroutine. The returned Recorder is ready to receive Push immediately.
func Start(dir string, sampleRate int) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, serr.Wrap(err, "dir", dir)
	}
	// Take names keep the second-resolution timestamp people already see in
	// their recordings folder; a take that would collide (stop and restart
	// within one second) gets a -2, -3, … suffix. newWavWriter opens with
	// O_EXCL, so the existence check and the create are one atomic step —
	// no window in which a second start could claim the same name.
	stamp := "bounce-" + time.Now().Format("20060102-150405")
	var (
		w    *wavWriter
		path string
		err  error
	)
	for n := 1; n <= maxTakeSuffix; n++ {
		path = filepath.Join(dir, stamp+".wav")
		if n > 1 {
			path = filepath.Join(dir, stamp+"-"+strconv.Itoa(n)+".wav")
		}
		w, err = newWavWriter(path, sampleRate)
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
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
// Frames are counted only when the ring accepted them, so Seconds reports
// what will actually be in the file rather than wall time.
func (rec *Recorder) Push(interleaved []float32) {
	if rec.ring.Push(interleaved) {
		rec.frames.Add(uint64(len(interleaved) / 2))
	}
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
				rec.failErr.Store(&err)
				return false
			}
		}
	}

	for polls := 1; !rec.stop.Load(); polls++ {
		if !flush() {
			// Finalize what made it to disk: the header is patched to the
			// samples actually written, so the partial take stays playable.
			if err := w.close(); err != nil {
				logger.LogErr(err, "msg", "closing WAV file after write failure")
			}
			return
		}
		if polls%checkpointEvery == 0 {
			if err := w.checkpoint(); err != nil {
				logger.LogErr(err, "msg", "recording checkpoint failed; stopping drain")
				rec.failErr.Store(&err)
				w.close()
				return
			}
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

// Dropped reports how many blocks the audio thread had to drop because the
// ring was full (disk could not keep up). Each is an audible gap in the
// file, so the UI reports it at stop. Safe from any goroutine.
func (rec *Recorder) Dropped() uint64 { return rec.ring.Overruns() }

// Err returns the error that stopped the drain early, or nil while the take
// is healthy. Safe from any goroutine.
func (rec *Recorder) Err() error {
	if p := rec.failErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Seconds returns the elapsed recorded duration so far.
func (rec *Recorder) Seconds() float64 {
	return float64(rec.frames.Load()) / float64(rec.rate)
}
