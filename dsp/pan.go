package dsp

import "math"

// PanGains maps a pan position in [-1 (hard left) .. +1 (hard right)] to
// left/right gain multipliers using the constant-power law:
//
//	θ = (pan + 1)·π/4        (0 .. π/2)
//	gL = cos θ,  gR = sin θ
//
// Constant-power is chosen over linear pan because gL²+gR² == 1 everywhere,
// so perceived loudness stays even across the sweep. Linear pan (gL=1-t,
// gR=t) dips ~3dB at center, making centered sources sound recessed.
func PanGains(pan float64) (gL, gR float64) {
	if pan < -1 {
		pan = -1
	} else if pan > 1 {
		pan = 1
	}
	theta := (pan + 1) * math.Pi / 4
	s, c := math.Sincos(theta)
	return c, s
}
