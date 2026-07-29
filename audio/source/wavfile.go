package source

import (
	"encoding/binary"
	"math"
	"os"
	"sync/atomic"

	"github.com/rohanthewiz/serr"
)

// WavSource plays a WAV file that was fully decoded into memory at load
// time. Decoding up front (rather than streaming) is a deliberate trade:
// mixer stems are small relative to RAM, and it guarantees the audio thread
// never touches the disk — the one operation with unbounded latency.
type WavSource struct {
	path string
	l, r []float32 // decoded, engine-rate stereo
	pos  int
	Loop atomic.Bool
}

// LoadWav decodes a PCM WAV (16/24-bit int or 32-bit float, mono or stereo)
// and linearly resamples it to engineRate if needed.
func LoadWav(path string, engineRate float64) (*WavSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, serr.Wrap(err, "path", path)
	}

	l, r, fileRate, err := decodeWav(data)
	if err != nil {
		return nil, serr.Wrap(err, "path", path)
	}

	if fileRate != engineRate {
		l = resampleLinear(l, fileRate, engineRate)
		r = resampleLinear(r, fileRate, engineRate)
	}

	ws := &WavSource{path: path, l: l, r: r}
	ws.Loop.Store(true) // mixer sources default to looping; a one-shot that ends leaves a dead channel
	return ws, nil
}

// Read copies the next block; loops or goes silent at EOF. Audio thread.
func (w *WavSource) Read(l, r []float32) int {
	n := 0
	for n < len(l) {
		if w.pos >= len(w.l) {
			if !w.Loop.Load() {
				break
			}
			w.pos = 0
		}
		remain := len(w.l) - w.pos
		want := len(l) - n
		if want > remain {
			want = remain
		}
		copy(l[n:n+want], w.l[w.pos:w.pos+want])
		copy(r[n:n+want], w.r[w.pos:w.pos+want])
		n += want
		w.pos += want
	}
	return n
}

func (w *WavSource) Name() string { return "wav:" + w.path }

// Rewind restarts playback from the top (control plane; a mid-block reset
// race just replays a few samples, which is harmless).
func (w *WavSource) Rewind() { w.pos = 0 }

// decodeWav walks RIFF chunks and converts the data chunk to stereo float32.
// Only the chunks we need are parsed; unknown chunks are skipped by size,
// which is what keeps this parser robust against the many WAV variants
// (LIST/INFO metadata, fact chunks, etc.).
func decodeWav(data []byte) (l, r []float32, sampleRate float64, err error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, nil, 0, serr.New("not a RIFF/WAVE file")
	}

	var numChannels, bitsPerSample int
	var audioFormat int
	var raw []byte

	pos := 12
	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		if body+chunkSize > len(data) {
			chunkSize = len(data) - body // tolerate truncated final chunk
		}

		switch chunkID {
		case "fmt ":
			if chunkSize < 16 {
				return nil, nil, 0, serr.New("fmt chunk too small")
			}
			audioFormat = int(binary.LittleEndian.Uint16(data[body : body+2]))
			numChannels = int(binary.LittleEndian.Uint16(data[body+2 : body+4]))
			sampleRate = float64(binary.LittleEndian.Uint32(data[body+4 : body+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[body+14 : body+16]))
		case "data":
			raw = data[body : body+chunkSize]
		}

		// Chunks are word-aligned: odd sizes are padded with one byte.
		pos = body + chunkSize
		if chunkSize%2 == 1 {
			pos++
		}
	}

	if raw == nil {
		return nil, nil, 0, serr.New("no data chunk found")
	}
	if numChannels < 1 || numChannels > 2 {
		return nil, nil, 0, serr.New("unsupported channel count", "channels", string(rune('0'+numChannels)))
	}

	// WAVE_FORMAT_EXTENSIBLE (0xFFFE) wraps PCM/float; we treat it by bit
	// depth like the plain formats, which covers the common cases.
	const fmtPCM, fmtFloat, fmtExtensible = 1, 3, 0xFFFE
	if audioFormat != fmtPCM && audioFormat != fmtFloat && audioFormat != fmtExtensible {
		return nil, nil, 0, serr.New("unsupported WAV format code")
	}

	bytesPerSample := bitsPerSample / 8
	frames := len(raw) / (bytesPerSample * numChannels)
	l = make([]float32, frames)
	r = make([]float32, frames)

	for f := 0; f < frames; f++ {
		for ch := 0; ch < numChannels; ch++ {
			off := (f*numChannels + ch) * bytesPerSample
			var v float32
			switch bitsPerSample {
			case 16:
				v = float32(int16(binary.LittleEndian.Uint16(raw[off:]))) / 32768
			case 24:
				// 24-bit little-endian: assemble into the top of an int32
				// then shift back down to sign-extend.
				u := uint32(raw[off]) | uint32(raw[off+1])<<8 | uint32(raw[off+2])<<16
				v = float32(int32(u<<8)>>8) / 8388608
			case 32:
				if audioFormat == fmtFloat || audioFormat == fmtExtensible {
					v = math.Float32frombits(binary.LittleEndian.Uint32(raw[off:]))
				} else {
					v = float32(int32(binary.LittleEndian.Uint32(raw[off:]))) / 2147483648
				}
			default:
				return nil, nil, 0, serr.New("unsupported bit depth")
			}
			if ch == 0 {
				l[f] = v
				if numChannels == 1 {
					r[f] = v // mono fans out to both sides
				}
			} else {
				r[f] = v
			}
		}
	}
	return l, r, sampleRate, nil
}

// resampleLinear converts between sample rates with linear interpolation.
// Linear (vs windowed-sinc) trades a little top-octave fidelity for tiny,
// obviously-correct code — acceptable for v1 file playback.
func resampleLinear(in []float32, fromRate, toRate float64) []float32 {
	if len(in) == 0 {
		return in
	}
	ratio := fromRate / toRate
	outLen := int(float64(len(in)) / ratio)
	out := make([]float32, outLen)
	for i := range out {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := float32(srcPos - float64(idx))
		if idx+1 < len(in) {
			out[i] = in[idx]*(1-frac) + in[idx+1]*frac
		} else {
			out[i] = in[idx]
		}
	}
	return out
}
