package record

import "sync/atomic"

// f32Ring is a single-producer/single-consumer lock-free ring buffer of
// float32 samples. The producer is the audio callback (recording tap); the
// consumer is the recorder's drain goroutine. SPSC is the one ring topology
// that needs no CAS loops: each side owns exactly one index, so a plain
// atomic load of the *other* side's index gives a safe conservative view.
//
// Indices increase monotonically and are masked onto the power-of-two
// buffer; monotonic counters sidestep the classic "full vs empty look the
// same" ambiguity without sacrificing a slot.
//
// On overflow the producer DROPS the block and counts it. Dropping is the
// only realtime-safe choice — blocking the audio thread to preserve a
// recording would trade a file glitch for an audible one.
type f32Ring struct {
	buf      []float32
	mask     uint64
	w        atomic.Uint64 // written samples (producer-owned)
	r        atomic.Uint64 // read samples (consumer-owned)
	overruns atomic.Uint64
}

// newF32Ring rounds capacity up to a power of two. 1<<18 samples ≈ 1.3s of
// stereo 48k — enough slack to ride out any plausible disk stall.
func newF32Ring(minCapacity int) *f32Ring {
	capacity := 1
	for capacity < minCapacity {
		capacity <<= 1
	}
	return &f32Ring{
		buf:  make([]float32, capacity),
		mask: uint64(capacity - 1),
	}
}

// Push appends samples; drops the whole slice if it doesn't fit (audio thread).
func (rb *f32Ring) Push(p []float32) {
	w := rb.w.Load()
	r := rb.r.Load()
	if int(w-r)+len(p) > len(rb.buf) {
		rb.overruns.Add(1)
		return
	}
	for _, s := range p {
		rb.buf[w&rb.mask] = s
		w++
	}
	rb.w.Store(w) // publish after the data is in place
}

// Pop fills p with up to len(p) samples, returning how many were read
// (consumer goroutine).
func (rb *f32Ring) Pop(p []float32) int {
	r := rb.r.Load()
	w := rb.w.Load()
	n := int(w - r)
	if n > len(p) {
		n = len(p)
	}
	for i := 0; i < n; i++ {
		p[i] = rb.buf[r&rb.mask]
		r++
	}
	rb.r.Store(r)
	return n
}

// Overruns reports how many pushes were dropped due to a full ring.
func (rb *f32Ring) Overruns() uint64 { return rb.overruns.Load() }
