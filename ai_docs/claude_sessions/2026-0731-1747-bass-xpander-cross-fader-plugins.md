# Session: Bass Xpander & Cross Fader plugins + plugin-creation docs

- **Session ID**: `9acd3e47-76ea-439e-b555-afa62ac0776c`
- **Date**: 2026-07-31 17:47
- **Branch**: `main`

## What was done

### 1. Bass Xpander plugin (`plugins/src/bass_xpander/main.go`) — committed & pushed as `747b568`

Created as a worked example of adding a new external plugin. A bass enhancer
with two parallel generators under a crossover, dry path untouched:

- **Harmonic excitation**: low band (two cascaded one-pole LPFs ≈ 12 dB/oct)
  through `tanh(drive·x)/drive`; only the *difference* (saturated − clean) is
  mixed in, so the Harmonics knob adds purely generated content. The `/drive`
  normalization makes Drive change color, not loudness.
- **Sub-octave synthesis**: flip-flop divider toggling on rising
  zero-crossings of the mono-summed low band → square at half the
  fundamental; low-passed toward a sine and scaled by a fast-attack (~5 ms)
  / slow-release (~80 ms) envelope follower so the sub tracks dynamics.
  Hysteresis threshold (1e-4) prevents noise-floor chatter on the divider.

Params: `xover` (50–400 Hz, log), `drive` (1–10), `amount` (0–1), `sub` (0–1).
All state is fixed-size struct fields — zero allocations in `Process`,
per-block (not per-sample) atomic param reads.

### 2. README plugin-creation walkthrough — same commit `747b568`

Rewrote "Writing an audio module plugin" into a 5-step walkthrough using
Bass Xpander: source under `plugins/src/<name>/`, `dsp.ParamCell`-backed
params, the five `AudioModule` methods with realtime rules, the exact
`-buildmode=plugin` build command, restart + verification via
`go run ./arch_test_scripts/plugin_smoke`. Kept the existing toolchain-identity
gotchas subsection unchanged.

### 3. Cross Fader plugin (`plugins/src/cross_fader/main.go`) — uncommitted this session, committed by the wrap-up

DJ-style fades: single-track fade up/down, or a linked crossfade across
multiple tracks.

**Key design decision**: `AudioModule` is a per-channel insert and can't see
other channels' audio — but one `.so` is loaded once per process, so its
package-level state is shared by all instances. The X-Fade `position` lives
in a **package-level `dsp.ParamCell`**; setting it from any channel's UI
moves the crossfade for every cross_fader instance. Per-instance params
control the response:

- `side` (0=A, 1=B): A-side tracks are loud at position 0 and fade out toward
  1; B-side is the mirror. Fractional values blend the two gain laws.
- `depth`: 1 = fade to silence, 0.5 ≈ 6 dB duck, 0 = bypass.
- `fade` (0–10 s): audio thread slews toward the shared target at this rate —
  flipping position with fade=3 s yields a timed 3 s auto-fade.

DSP details: equal-power law (`cos`/`sin` of position·π/2) avoids the −3 dB
midpoint hole of a linear crossfade; gain is ramped linearly across each
block (console idiom, two trig calls per block); a `primed` flag snaps the
first block's gain so mid-playback insertion doesn't fade in from silence.

Known quirk: because `position` is linked, other channels' UI sliders show a
stale position until their panel refreshes; audio responds immediately.

Also added cross_fader to the README layout line.

## Plugin-creation recipe (the answer to this session's opening question)

1. `plugins/src/<name>/main.go` — `package main`, empty `func main()`,
   exported `func NewModule() module.AudioModule`.
2. Implement `Name`, `Init` (allocate everything here), `Process`
   (realtime: no alloc/lock/log/block; read params once per block),
   `Params`, `SetParam`/`GetParam` (back with `dsp.ParamCell`).
3. `go build -buildmode=plugin -o plugins/<name>.so ./plugins/src/<name>`
   — must be built in-repo (toolchain + go.mod identity); rebuild all `.so`
   after any go.mod change.
4. Restart godaw — loader globs `plugins/*.so`, registers via `NewModule`;
   UI dropdown, sliders, and scene persistence come free. No registration
   code anywhere.
5. Verify: `go run ./arch_test_scripts/plugin_smoke`.

## Verification done

- `go vet` clean on both plugins (fixed one `rangeint` lint in cross_fader).
- `go build ./...` passes.
- Smoke test loads all three `.so` plugins and processes a block through
  every module: `available: [bass_xpander cross_fader delay flanger tremolo]`.

## Notes / possible next steps

- `plugins/*.so` is gitignored (correct — binaries must be rebuilt
  per-machine due to toolchain identity); only sources are committed.
- Possible follow-up: SSE-push param changes so linked cross_fader sliders
  update live across channel panels.
- Possible follow-up: a dedicated crossfader UI control (big horizontal
  fader) instead of the auto-generated vertical slider.
