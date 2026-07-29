# Session: go-daw initial build

- **Session ID**: `bb4db145-2ef0-4ec2-91eb-d8b9291268b6`
- **Date**: 2026-07-29
- **Result**: Complete working DAW/mixer, all features verified end-to-end

## What was built

A web-controlled digital audio workstation (`github.com/rohanthewiz/go-daw`) at `~/projs/go/go-daw`, from an empty directory to a verified binary in one session.

### Requirements (user)
- Configurable channel count; per-channel: input gain, L/R pan, 3 parametric EQs, low/high pass filters, compressor/limiter, reverb with configurable pre-delay, noise gate
- Audio module plugin support; grouping; digital memory
- Stack: RWeb, Element, SErr, Logger, go-styl

### Decisions (via Q&A + mid-session message)
- **Audio I/O**: full duplex via `gen2brain/malgo` (miniaudio cgo, vendored C, no brew). Fallback to playback-only if duplex init/mic permission fails.
- **Plugins**: builtin registry (tremolo, delay) AND runtime `.so` loading (`go build -buildmode=plugin`); example flanger in `plugins/src/flanger/`.
- **Digital memory**: named scene save/recall **plus** master-bus WAV recording.
- **Persistence**: `rohanthewiz/bytdb` — user explicitly overrode the global DuckDB default mid-planning. Pure Go, so malgo is the only cgo dep.

## Architecture highlights

- **Realtime contract**: audio callback never allocates/locks/logs. Control-plane ↔ audio-thread exchange via `dsp.ParamCell` (atomic float64 cells) and copy-on-write `atomic.Pointer`s (source swaps, module chains, biquad coefficient sets). No mutexes anywhere on the audio path (priority-inversion avoidance).
- **Signal flow**: source → gain(ramped) → gate → HPF → LPF → EQ×3 → comp → modules → reverb(insert w/ pre-delay) → pan(ramped) → group|master → safety limiter → out.
- **DSP**: RBJ biquads (TDF-II, f64 state), soft-knee compressor w/ branching envelope follower, 5-state gate w/ hysteresis, Freeverb-style reverb (4 combs + 2 allpasses + pre-delay line, +23-sample stereo spread), constant-power pan.
- **Recording**: master tap → SPSC lock-free ring (monotonic indices) → drain goroutine → streaming 16-bit WAV with header patch on close.
- **Scenes**: single bytdb table `scenes(name TEXT PK, updated_at INT, state TEXT)`; JSON snapshot documents; probe `information_schema.tables` before CREATE (bytdb has no `IF NOT EXISTS` for tables); upsert via `ON CONFLICT (name) DO UPDATE`.
- **Web**: rweb SSEHub broadcasts meters ~12 Hz; element renders whole console server-side (structural changes reload the page); go-styl compiles `web/styles/main.styl` once at startup; delegated single-listener vanilla JS (~200 lines) with 30 ms debounce and log-slider mapping mirrored Go↔JS.

## Package layout

```
audio/   engine.go (malgo duplex callback, block dispatch), convert.go, source/ (osc, wav, live)
dsp/     param, biquad, gate, dynamics, reverb, pan (+ sanity tests)
mixer/   channel, group, master, meter (packed atomics), state (Console, Snapshot/ApplyScene)
module/  interface, registry, loader (.so), builtin/ (tremolo, delay)
record/  ringbuf (SPSC), wavwriter, recorder
store/   scenes.go (bytdb) + round-trip test
web/     server.go (routes, SSE meter loop), handlers.go, ui/ (element components), styles/, assets/app.js
```

## Verification performed

1. `go test ./...` — DSP sanity (EQ +12dB at f0, LPF −40dB stopband, compressor GR, gate close, reverb tail, constant-power pan) + scene store round-trip: all pass.
2. Engine smoke: audible 220 Hz tone through full chain on CoreAudio; meters registered.
3. Plugin smoke: `flanger.so` built and loaded; `Available() = [delay flanger tremolo]`.
4. Record smoke: 1.5 s bounce validated with `afinfo` (2ch/48k/Int16).
5. Full HTTP e2e on `:8123`: page/CSS/JS render, param set + validation errors, group assign, module add/param/remove, scene save→wiggle→recall (gain and module chain restored), record start/stop, SSE meter frames correct.
6. Chrome-extension visual check skipped (extension not connected).

## Gotchas / notes for future sessions

- bytdb requires go ≥ 1.26.1 → go.mod bumped from 1.25.4 automatically.
- Plugin `.so` must be rebuilt after ANY go.mod change (Go plugin toolchain identity); sources live in-repo so they share go.mod.
- `plugins/src/flanger` has an empty `func main()` so `go build ./...` passes; plugin mode ignores it.
- First duplex run prompts for mic permission; denial → auto playback-only + UI badge.
- Smoke-test programs archived in `arch_test_scripts/` (moved from `test_scripts/` per convention).
- Root `assets/` was removed; app.js is embedded from `web/assets/`.

## Current state

Binary `godaw` builds clean; run `./godaw`, open http://localhost:8000. `godaw.db` contains test scene `e2e-test`; `recordings/` has e2e bounces (both gitignored).
