package dsp

// NonFinite reports whether any sample in l or r is NaN or ±Inf. Audio-thread
// safe: no allocation, no branches beyond the loop.
//
// Why this exists: every recursive processor in this package (biquad state,
// envelope followers, reverb combs) feeds its output back into itself. One
// non-finite sample — from a third-party .so plugin, a corrupt float WAV, or
// a filter pushed past stability — is therefore not a one-off glitch: it
// latches into that state forever. Downstream it is worse still: the master
// limiter's envelope goes NaN (the whole console falls silent until restart)
// and a NaN meter level makes encoding/json refuse the SSE frame, freezing
// every open tab's meters. Callers use this check to contain a fault to the
// strip that produced it.
//
// The x-x != 0 test covers both failure kinds in one compare: it is exactly 0
// for every finite x, and NaN (which compares unequal to everything) for NaN
// and for ±Inf (Inf-Inf = NaN). It avoids math.IsNaN/IsInf's float64
// conversions in what is otherwise a tight float32 scan.
func NonFinite(l, r []float32) bool {
	for i := range l {
		if l[i]-l[i] != 0 || r[i]-r[i] != 0 {
			return true
		}
	}
	return false
}

// Finite reports whether v is neither NaN nor ±Inf. Control-plane helper for
// validating parameter values before they reach a ParamCell: clamping alone
// does not help, because every comparison against NaN is false, so a NaN
// sails straight through a min/max clamp.
func Finite(v float64) bool { return v-v == 0 }
