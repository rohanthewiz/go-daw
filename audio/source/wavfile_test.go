package source

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// wavBytes assembles a minimal RIFF/WAVE file. fmtExtra is appended after
// the 16-byte basic fmt body (the WAVE_FORMAT_EXTENSIBLE extension goes
// there); samples is the raw data chunk.
func wavBytes(format, channels, rate, bits int, fmtExtra, samples []byte) []byte {
	fmtBody := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtBody[0:], uint16(format))
	binary.LittleEndian.PutUint16(fmtBody[2:], uint16(channels))
	binary.LittleEndian.PutUint32(fmtBody[4:], uint32(rate))
	blockAlign := channels * bits / 8
	binary.LittleEndian.PutUint32(fmtBody[8:], uint32(rate*blockAlign))
	binary.LittleEndian.PutUint16(fmtBody[12:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(fmtBody[14:], uint16(bits))
	fmtBody = append(fmtBody, fmtExtra...)

	var b []byte
	chunk := func(id string, body []byte) {
		var h [8]byte
		copy(h[:4], id)
		binary.LittleEndian.PutUint32(h[4:], uint32(len(body)))
		b = append(b, h[:]...)
		b = append(b, body...)
		if len(body)%2 == 1 {
			b = append(b, 0)
		}
	}
	b = append(b, []byte("RIFF\x00\x00\x00\x00WAVE")...)
	chunk("fmt ", fmtBody)
	chunk("data", samples)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	return b
}

// extensible builds the 24-byte WAVE_FORMAT_EXTENSIBLE fmt extension whose
// SubFormat GUID begins with the given format code.
func extensible(subFormat, validBits int) []byte {
	ext := make([]byte, 24)
	binary.LittleEndian.PutUint16(ext[0:], 22) // cbSize
	binary.LittleEndian.PutUint16(ext[2:], uint16(validBits))
	binary.LittleEndian.PutUint16(ext[8:], uint16(subFormat)) // GUID data1 low word
	return ext
}

func writeTemp(t *testing.T, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.wav")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLoadWavRejectsMalformedHeaders: each of these used to panic (crashing
// the process from an HTTP handler) or produce a source that hangs the
// audio thread. Each must now come back as an ordinary error.
func TestLoadWavRejectsMalformedHeaders(t *testing.T) {
	two16 := []byte{0, 0, 0, 0}
	cases := map[string][]byte{
		"empty data chunk (unfinalized bounce)": wavBytes(1, 2, 48000, 16, nil, nil),
		"bits 4 (was divide by zero)":           wavBytes(1, 2, 48000, 4, nil, two16),
		"bits 0 (was divide by zero)":           wavBytes(1, 1, 48000, 0, nil, two16),
		"rate 0 (was make(int(+Inf)))":          wavBytes(1, 2, 0, 16, nil, two16),
		"8-bit PCM (never decoded)":             wavBytes(1, 1, 48000, 8, nil, two16),
		"64-bit float":                          wavBytes(3, 1, 48000, 64, nil, make([]byte, 16)),
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("LoadWav panicked: %v", r)
				}
			}()
			if _, err := LoadWav(writeTemp(t, b), 48000); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadWavRejectsNonRegularFile(t *testing.T) {
	if _, err := LoadWav(t.TempDir(), 48000); err == nil {
		t.Fatal("a directory was accepted as a WAV")
	}
	if _, err := os.Stat("/dev/zero"); err == nil {
		done := make(chan error, 1)
		go func() { _, err := LoadWav("/dev/zero", 48000); done <- err }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("/dev/zero was accepted")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("LoadWav(/dev/zero) did not return (unbounded read)")
		}
	}
}

// TestDecodeExtensibleInt32: >16-bit integer PCM written as
// WAVE_FORMAT_EXTENSIBLE must decode as integers, not be reinterpreted as
// IEEE floats.
func TestDecodeExtensibleInt32(t *testing.T) {
	s := make([]byte, 8)
	half, negHalf := int32(1<<30), int32(-(1 << 30))
	binary.LittleEndian.PutUint32(s[0:], uint32(half))    // +0.5
	binary.LittleEndian.PutUint32(s[4:], uint32(negHalf)) // -0.5
	l, r, rate, err := decodeWav(wavBytes(0xFFFE, 2, 48000, 32, extensible(1, 32), s))
	if err != nil {
		t.Fatal(err)
	}
	if rate != 48000 || len(l) != 1 || l[0] != 0.5 || r[0] != -0.5 {
		t.Fatalf("got rate=%v l=%v r=%v, want 0.5/-0.5", rate, l, r)
	}

	// Extensible float still decodes as float.
	binary.LittleEndian.PutUint32(s[0:], math.Float32bits(0.25))
	binary.LittleEndian.PutUint32(s[4:], math.Float32bits(-0.75))
	l, r, _, err = decodeWav(wavBytes(0xFFFE, 2, 48000, 32, extensible(3, 32), s))
	if err != nil || l[0] != 0.25 || r[0] != -0.75 {
		t.Fatalf("extensible float: %v %v %v", l, r, err)
	}
}

// TestDecodeFloatNonFiniteIsSilence: Inf/NaN in a float WAV would fault the
// strip on every loop pass; it decodes as 0 instead.
func TestDecodeFloatNonFiniteIsSilence(t *testing.T) {
	s := make([]byte, 12)
	binary.LittleEndian.PutUint32(s[0:], math.Float32bits(float32(math.NaN())))
	binary.LittleEndian.PutUint32(s[4:], math.Float32bits(float32(math.Inf(1))))
	binary.LittleEndian.PutUint32(s[8:], math.Float32bits(0.5))
	l, _, _, err := decodeWav(wavBytes(3, 1, 48000, 32, nil, s))
	if err != nil {
		t.Fatal(err)
	}
	if l[0] != 0 || l[1] != 0 || l[2] != 0.5 {
		t.Fatalf("got %v, want [0 0 0.5]", l)
	}
}

// TestWavReadEmptyDoesNotSpin: the audio-thread Read must return even on a
// source with no frames and Loop on (previously an infinite loop).
func TestWavReadEmptyDoesNotSpin(t *testing.T) {
	w := &WavSource{}
	w.Loop.Store(true)
	done := make(chan int, 1)
	go func() { done <- w.Read(make([]float32, 256), make([]float32, 256)) }()
	select {
	case n := <-done:
		if n != 0 {
			t.Fatalf("Read = %d, want 0", n)
		}
	case <-time.After(time.Second):
		t.Fatal("Read on an empty looping source never returned")
	}
}

// TestLoadWavResamples covers the happy path end to end, including a rate
// conversion, so the new validation did not reject good files.
func TestLoadWavResamples(t *testing.T) {
	s := make([]byte, 2*2*441) // 441 stereo 16-bit frames at 44.1k = 10 ms
	ws, err := LoadWav(writeTemp(t, wavBytes(1, 2, 44100, 16, nil, s)), 48000)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.l) != 480 {
		t.Fatalf("resampled to %d frames, want 480", len(ws.l))
	}
}
