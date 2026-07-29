package mixer

import (
	"github.com/rohanthewiz/go-daw/dsp"
)

// Master is the final bus: master gain, then an always-on safety limiter.
// The limiter matters because N channels summing can easily exceed 0dBFS,
// and float32 headroom means the overflow survives the mix bus fine — right
// up until it hits the DAC's fixed-point world and wraps into harsh digital
// clipping. A brickwall at -0.3dB turns that failure mode into graceful,
// momentary gain reduction.
type Master struct {
	GainDB  *dsp.ParamCell
	Limiter *dsp.Compressor
	Meter   Meter

	L, R     []float32
	lastGain float64
}

// NewMaster builds the master bus with its safety limiter engaged.
func NewMaster(sampleRate float64, maxBlock int) *Master {
	return &Master{
		GainDB:   dsp.NewParam(0),
		Limiter:  dsp.NewLimiter(sampleRate),
		L:        make([]float32, maxBlock),
		R:        make([]float32, maxBlock),
		lastGain: 1,
	}
}

// Clear zeroes the accumulators at the top of a block. Audio thread.
func (m *Master) Clear(n int) {
	for i := 0; i < n; i++ {
		m.L[i], m.R[i] = 0, 0
	}
}

// ProcessBlock applies gain and the safety limiter, then meters.
// Audio thread.
func (m *Master) ProcessBlock(n int) (l, r []float32) {
	l, r = m.L[:n], m.R[:n]

	target := dsp.DBToLin(m.GainDB.Get())
	step := (target - m.lastGain) / float64(n)
	gain := m.lastGain
	for i := range l {
		gain += step
		l[i] *= float32(gain)
		r[i] *= float32(gain)
	}
	m.lastGain = target

	m.Limiter.ProcessBlock(l, r)
	m.Meter.StoreBlock(l, r)
	return l, r
}
