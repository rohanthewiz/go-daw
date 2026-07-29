// Package record captures the master bus to WAV files on disk. The audio
// thread pushes into a lock-free ring; a drain goroutine owns all file I/O.
package record

import (
	"bufio"
	"encoding/binary"
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
}

const wavHeaderSize = 44

func newWavWriter(path string, sampleRate int) (*wavWriter, error) {
	f, err := os.Create(path)
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
	var b [2]byte
	for _, s := range p {
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		binary.LittleEndian.PutUint16(b[:], uint16(int16(s*32767)))
		if _, err := w.bw.Write(b[:]); err != nil {
			return serr.Wrap(err)
		}
	}
	w.samples += len(p)
	return nil
}

// close flushes, patches the header sizes, and closes the file.
func (w *wavWriter) close() error {
	if err := w.bw.Flush(); err != nil {
		w.f.Close()
		return serr.Wrap(err)
	}

	dataBytes := uint32(w.samples * 2)
	var b4 [4]byte

	binary.LittleEndian.PutUint32(b4[:], 36+dataBytes)
	if _, err := w.f.WriteAt(b4[:], 4); err != nil {
		w.f.Close()
		return serr.Wrap(err, "msg", "patching RIFF size")
	}
	binary.LittleEndian.PutUint32(b4[:], dataBytes)
	if _, err := w.f.WriteAt(b4[:], 40); err != nil {
		w.f.Close()
		return serr.Wrap(err, "msg", "patching data size")
	}
	if err := w.f.Close(); err != nil {
		return serr.Wrap(err)
	}
	return nil
}
