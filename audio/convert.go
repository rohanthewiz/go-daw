// Package audio hosts the realtime engine: device I/O via miniaudio (malgo),
// the block-processing loop that drives the mixer console, and the
// lock-free ring buffer used to hand audio to the recorder.
package audio

import "unsafe"

// f32View reinterprets an interleaved FormatF32 byte buffer from miniaudio
// as a []float32 without copying. This is safe because miniaudio allocates
// its period buffers with at least 4-byte alignment and the format is
// explicitly requested as F32 — the bytes *are* float32s; Go just doesn't
// know it. Doing the reinterpretation once per callback (instead of a
// per-sample decode) keeps the device boundary free.
func f32View(b []byte, frames, chans int) []float32 {
	if len(b) == 0 || frames*chans == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), frames*chans)
}

// deinterleave splits [L R L R ...] into separate L and R buffers.
func deinterleave(in []float32, l, r []float32, frames int) {
	for i := 0; i < frames; i++ {
		l[i] = in[i*2]
		r[i] = in[i*2+1]
	}
}

// interleave merges L and R buffers into [L R L R ...].
func interleave(out []float32, l, r []float32, frames int) {
	for i := 0; i < frames; i++ {
		out[i*2] = l[i]
		out[i*2+1] = r[i]
	}
}
