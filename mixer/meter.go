package mixer

import (
	"math"
	"sync/atomic"
)

// Meter publishes per-side peak and RMS levels from the audio thread to the
// UI. Both float32 values for one side are packed into a single
// atomic.Uint64 (peak in the high 32 bits, RMS in the low 32) so a reader
// can never observe a peak from one block paired with an RMS from another.
type Meter struct {
	l, r atomic.Uint64
}

func pack(peak, rms float32) uint64 {
	return uint64(math.Float32bits(peak))<<32 | uint64(math.Float32bits(rms))
}

func unpack(v uint64) (peak, rms float32) {
	return math.Float32frombits(uint32(v >> 32)), math.Float32frombits(uint32(v))
}

// StoreBlock computes and publishes levels for one block. Audio thread.
// Peak decays exponentially across blocks (~0.92 per block) so the UI meter
// falls smoothly instead of snapping to zero between transients.
func (m *Meter) StoreBlock(l, r []float32) {
	prevPeakL, _ := unpack(m.l.Load())
	prevPeakR, _ := unpack(m.r.Load())
	m.l.Store(pack(blockLevels(l, prevPeakL)))
	m.r.Store(pack(blockLevels(r, prevPeakR)))
}

func blockLevels(buf []float32, prevPeak float32) (peak, rms float32) {
	decayed := prevPeak * 0.92
	sum := 0.0
	peak = 0
	for _, s := range buf {
		if s < 0 {
			s = -s
		}
		if s > peak {
			peak = s
		}
		sum += float64(s) * float64(s)
	}
	if decayed > peak {
		peak = decayed
	}
	rms = float32(math.Sqrt(sum / float64(max(len(buf), 1))))
	return peak, rms
}

// Levels returns (peakL, rmsL, peakR, rmsR). Wait-free; safe from any goroutine.
func (m *Meter) Levels() (pl, rl, pr, rr float32) {
	pl, rl = unpack(m.l.Load())
	pr, rr = unpack(m.r.Load())
	return
}
