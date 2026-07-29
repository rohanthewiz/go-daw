// Package dsp contains the self-contained signal processors used by every
// channel strip: parameter cells, biquad filters (parametric EQ, LPF, HPF),
// noise gate, compressor/limiter, Schroeder reverb with pre-delay, and
// constant-power panning.
//
// Realtime contract shared by everything in this package: any method named
// Process* runs on the audio callback thread and must not allocate, lock,
// log, or block. All parameter exchange between the HTTP control plane and
// the audio thread happens through lock-free atomics.
package dsp

import (
	"math"
	"sync/atomic"
)

// ParamCell is a lock-free float64 parameter shared between the control
// plane (HTTP handlers write) and the audio thread (callback reads).
//
// Why not a mutex: a mutex shared with HTTP goroutines can priority-invert
// the CoreAudio realtime thread — a GC-preempted goroutine holding the lock
// becomes an audible dropout. An atomic read is wait-free; the worst case is
// the audio thread seeing a value one block (~5ms) stale, which is inaudible.
type ParamCell struct {
	v atomic.Uint64
}

// NewParam returns a cell initialized to val.
func NewParam(val float64) *ParamCell {
	p := &ParamCell{}
	p.Set(val)
	return p
}

// Set stores a new value (control plane).
func (p *ParamCell) Set(f float64) { p.v.Store(math.Float64bits(f)) }

// Get loads the current value (audio thread safe, wait-free).
func (p *ParamCell) Get() float64 { return math.Float64frombits(p.v.Load()) }

// DBToLin converts decibels to linear amplitude: 0dB -> 1.0, -6dB -> ~0.5.
func DBToLin(db float64) float64 { return math.Pow(10, db/20) }

// LinToDB converts linear amplitude to decibels, floored to avoid -Inf on
// silence (the tiny epsilon corresponds to about -180dB, far below hearing).
func LinToDB(lin float64) float64 { return 20 * math.Log10(lin+1e-9) }
