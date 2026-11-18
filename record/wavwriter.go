// Package record captures the master bus to WAV files on disk. The audio
// thread pushes into a lock-free ring; a drain goroutine owns all file I/O.
package record

import (
	"bufio"
	"encoding/binary"
	"math"
	"os"

	"github.com/rohanthewiz/serr"
)

// wavWriter streams 16-bit PCM stereo to disk. The RIFF header is written
// up front with placeholder sizes and patched on Close — the standard trick
// for streaming WAV, since the total length isn't known until stop.
//
// 16-bit is the v1 choice: universally readable, half the disk of 24-bit,
// and the master bus is already limited so the extra headroom of deeper
// formats buys little for a bounce.
type wavWriter struct {
	f       *os.File
	bw      *bufio.Writer
	samples int // total individual samples (not frames) written

	// pcm is reusable conversion scratch: a batch of float samples becomes
	// one []byte and one bufio.Write, instead of a 2-byte Write call per
	// sample. Grown on demand, never shrunk; the drain pops at most 8192
	// samples at a time, so it settles at 16 KiB.
	pcm []byte
}

const wavHeaderSize = 44

// maxWavDataBytes is the largest data chunk a RIFF WAV can describe: its
// size fields are uint32, and the RIFF size (36 + data) must fit too. At
// 48 kHz stereo 16-bit (192 kB/s) that is ~6.2 hours. Past it the header
// sizes would silently wrap and the file would play back as a few seconds
// of audio, so writeSamples refuses with errWavFull instead.
const maxWavDataBytes = math.MaxUint32 - 36

// errWavFull reports that the next write would overflow the WAV size
// fields. The drain treats it like any write error: stop, finalize, log.
var errWavFull = serr.New("WAV size limit reached (~4 GiB data chunk); recording stopped")

// newWavWriter creates path exclusively (O_EXCL): it fails with an error
// matching fs.ErrExist rather than truncating an existing file. The
// recorder relies on that to never overwrite a previous take; os.Create
// used to do exactly that when two takes started within the same second.
func newWavWriter(path string, sampleRate int) (*wavWriter, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, serr.Wrap(err, "path", path)
	}
	w := &wavWriter{f: f, bw: bufio.NewWriterSize(f, 1<<16)}

	const numChannels = 2
	const bitsPerSample = 16
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8

	var hdr [wavHeaderSize]byte
	copy(hdr[0:4], "RIFF")
	// hdr[4:8] = RIFF size, patched on close
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(hdr[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(hdr[22:24], numChannels)
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(hdr[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(hdr[34:36], bitsPerSample)
	copy(hdr[36:40], "data")
	// hdr[40:44] = data size, patched on close

	if _, err := w.bw.Write(hdr[:]); err != nil {
		f.Close()
		return nil, serr.Wrap(err, "path", path)
	}
	return w, nil
}

// writeSamples converts float32 samples to int16 with hard clamping.
// Clamping matters even after the master limiter: the limiter is not a true
// brickwall (its attack lets a fraction of a transient through), and an
// unclamped cast would wrap to the opposite polarity — the loudest possible
// artifact.
func (w *wavWriter) writeSamples(p []float32) error {
	if int64(w.samples+len(p))*2 > maxWavDataBytes {
		return errWavFull
	}
	need := len(p) * 2
	if cap(w.pcm) < need {
		w.pcm = make([]byte, need)
	}
	buf := w.pcm[:need]
	for i, s := range p {
		// The negated compare also catches NaN (every NaN comparison is
		// false), mapping it to silence instead of an implementation-defined
		// float→int conversion. The master bus guards against NaN already;
		// this keeps the writer safe on its own terms.
		if !(s >= -1) {
			if s != s {
				s = 0
			} else {
				s = -1
			}
		} else if s > 1 {
			s = 1
		}
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(int16(s*32767)))
	}
	if _, err := w.bw.Write(buf); err != nil {
		return serr.Wrap(err)
	}
	w.samples += len(p)
	return nil
}

// checkpoint makes the file on disk a complete, playable WAV of everything
// written so far: flush the buffer, then patch both size fields to the
// current length. The drain calls it about once a second.
//
// Without it the sizes stay at their placeholder zeros until close, so a
// crash, kill -9, or power loss mid-take leaves a file most players treat as
// empty, even though the audio is all there. With it, at most the last
// second is unaccounted for. WriteAt uses pwrite, which leaves the
// sequential write offset alone, so appending continues where it was.
func (w *wavWriter) checkpoint() error {
	if err := w.bw.Flush(); err != nil {
		return serr.Wrap(err)
	}
	return w.patchSizes()
}

// patchSizes writes the RIFF and data chunk sizes for the samples written
// so far. Callers flush first, so the sizes never claim unwritten bytes.
func (w *wavWriter) patchSizes() error {
	dataBytes := uint32(w.samples * 2)
	var b4 [4]byte

	binary.LittleEndian.PutUint32(b4[:], 36+dataBytes)
	if _, err := w.f.WriteAt(b4[:], 4); err != nil {
		return serr.Wrap(err, "msg", "patching RIFF size")
	}
	binary.LittleEndian.PutUint32(b4[:], dataBytes)
	if _, err := w.f.WriteAt(b4[:], 40); err != nil {
		return serr.Wrap(err, "msg", "patching data size")
	}
	return nil
}

// close flushes, patches the header sizes, and closes the file.
func (w *wavWriter) close() error {
	if err := w.bw.Flush(); err != nil {
		w.f.Close()
		return serr.Wrap(err)
	}
	if err := w.patchSizes(); err != nil {
		w.f.Close()
		return err
	}
	if err := w.f.Close(); err != nil {
		return serr.Wrap(err)
	}
	return nil
}
