// Package module defines the audio plugin contract. Both compiled-in
// modules (module/builtin) and external .so plugins (built with
// `go build -buildmode=plugin`) implement AudioModule; the mixer treats
// them identically as channel inserts.
//
// This package must stay dependency-light: external plugin authors import
// it, and every dependency here becomes a version that plugin builds must
// match exactly (Go plugin loading requires identical package versions).
package module

// ParamScale hints the UI how to map a slider onto the parameter range.
type ParamScale int

const (
	ScaleLinear ParamScale = iota
	// ScaleLog is for frequency-like params where equal slider travel
	// should cover equal octaves, not equal Hz.
	ScaleLog
)

// ParamSpec describes one automatable parameter so the web UI can
// auto-generate a control for it without knowing the module's type.
type ParamSpec struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Unit    string     `json:"unit"`
	Min     float64    `json:"min"`
	Max     float64    `json:"max"`
	Default float64    `json:"default"`
	Scale   ParamScale `json:"scale"`
}

// AudioModule is one stereo insert effect.
type AudioModule interface {
	// Name is the registry key and UI label.
	Name() string

	// Init is called once on the control plane before the module enters a
	// channel's chain. Allocate all buffers here, sized for maxBlock.
	Init(sampleRate float64, maxBlock int) error

	// Process runs on the audio thread: transform l/r in place
	// (len(l) == len(r) == current block). It must not allocate, lock,
	// log, or block.
	Process(l, r []float32)

	// Params describes the automatable parameters for UI generation.
	Params() []ParamSpec

	// SetParam / GetParam exchange values with the control plane.
	// Implementations should back these with atomic cells since Process
	// reads them from the audio thread.
	SetParam(id string, value float64)
	GetParam(id string) float64
}
