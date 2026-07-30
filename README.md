# go-daw

A web-controlled digital audio workstation / mixing console in Go for macOS.

```
┌─────────┐   HTTP/SSE   ┌─────────┐  atomics  ┌──────────────┐
│ browser │◄────────────►│  web    │◄─────────►│ audio engine │──► speakers
│ mixer UI│              │ (rweb)  │           │ (miniaudio)  │◄── mic/interface
└─────────┘              └────┬────┘           └──────┬───────┘
                              │ scenes JSON           │ master tap
                         ┌────▼────┐             ┌────▼─────┐
                         │  bytdb  │             │ recorder │──► WAV
                         └─────────┘             └──────────┘
```

## Features

- **Configurable channel count** (`config.json` or `-channels N`), plus group buses and a master bus with an always-on safety limiter.
- **Per-channel strip**: input gain, constant-power L/R pan, noise gate (hysteresis + attack/hold/release), high-pass and low-pass filters, 3 parametric EQ bands (RBJ biquads), compressor/limiter (soft knee, ratio ≥ 20 acts as a limiter), reverb with configurable pre-delay (Schroeder/Freeverb topology), mute.
- **Sources per channel**: live capture (full duplex), WAV file playback (16/24-bit PCM + float, auto-resampled), or a test oscillator.
- **Audio module plugins**: builtin registry (tremolo, delay) plus external Go plugins loaded from `plugins/*.so`. Parameter UIs are auto-generated from module metadata.
- **Grouping**: route any channel to a group bus (group gain/mute) or direct to master.
- **Digital memory**: named scenes snapshot the entire console to bytdb (pure-Go embedded DB); recall live without stopping audio. Plus master-bus recording to `recordings/*.wav`.
- **Virtual piano**: 25-key on-screen keyboard driving a polyphonic synth on any channel — playable by mouse/touch (strike position = velocity), computer keyboard (tracker layout, Z/X octave), or a hardware MIDI keyboard via Web MIDI (device picker, hot-plug, stuck-note panic on unplug).
- **Piano tutorial**: built-in guided lessons served from `/api/lessons` — scales, first melodies (Mary Had a Little Lamb, Twinkle Twinkle, Ode to Joy), and chord progressions. Target keys get amber guide rings, wrong notes flash red and are counted, chord steps require the full set held at once, and **Listen** plays the lesson through the synth. Any input route (mouse, keys, MIDI) can answer.
- **Web UI**: rweb server, element-rendered HTML, go-styl (Stylus) styling, SSE-driven meters (~12 Hz), tiny vanilla-JS client.

## Build & run

Requires Go ≥ 1.26 (module toolchain auto-switches) and Xcode Command Line Tools
(clang, for the miniaudio cgo bindings — the only cgo dependency).

```bash
go build -o godaw .
./godaw                      # open http://localhost:8000
./godaw -channels 12         # more channels
./godaw -no-input            # playback-only (skip mic capture)
./godaw -addr :9000 -sr 44100 -block 512
```

Defaults live in `config.json`; flags override the file.

### Microphone permission

The first duplex run makes macOS prompt for microphone access for your
terminal app. If it's denied, capture is silent — re-enable it under
**System Settings → Privacy & Security → Microphone**, or run with
`-no-input`. If duplex init fails for any reason the engine automatically
falls back to playback-only (the UI shows a `playback-only` badge).

Monitor live input on headphones — speakers will feed back into the mic.

## Writing an audio module plugin

A plugin is a `package main` that exports one symbol:

```go
func NewModule() module.AudioModule
```

See `plugins/src/flanger/` for a complete example. Build it with:

```bash
go build -buildmode=plugin -o plugins/flanger.so ./plugins/src/flanger
```

**Go plugin gotchas (read before debugging a load failure):**

- The `.so` must be built with the **exact same Go toolchain version and the
  identical versions of every shared dependency** as the `godaw` binary —
  including `go-daw/module` itself. The practical rule: keep plugin sources
  in this repo under `plugins/src/<name>/` so they build from the same
  `go.mod`, and rebuild every `.so` after any `go.mod` change.
- A mismatch shows up as `plugin was built with a different version of
  package …` at startup; the plugin is skipped (never fatal) and the app
  continues with builtin modules.
- Plugins can't be unloaded; removing a module from a channel only drops the
  instance.
- `Process(l, r []float32)` runs on the realtime audio thread: no
  allocation, locks, logging, or blocking. Exchange parameters through
  atomics (see `dsp.ParamCell`).

## Architecture notes

- **Realtime contract**: the miniaudio callback never allocates, locks, or
  logs. All control-plane ↔ audio-thread exchange is via atomic parameter
  cells and copy-on-write pointers (source swaps, module chains, biquad
  coefficient sets), so an HTTP request can never priority-invert the
  CoreAudio thread.
- **Signal flow**: source → gain → gate → HPF → LPF → EQ×3 → comp →
  modules → reverb → pan → (group | master) → limiter → output, with
  per-block linear ramps on gain/pan to prevent zipper noise.
- **Recording** taps the master bus into a lock-free SPSC ring; a drain
  goroutine owns all disk I/O and finalizes the WAV header on stop.
- **Scenes** are single JSON documents in one bytdb table (no FKs); save is
  an upsert, recall applies through the same atomic cells the UI uses.

## Layout

```
audio/        engine (malgo device + block dispatch), sources (osc/wav/live)
dsp/          biquad EQ/filters, gate, compressor, reverb, pan, ParamCell
mixer/        Channel/Group/Master strips, console, scene state
module/       AudioModule interface, registry, .so loader, builtins
record/       SPSC ring, WAV writer, recorder lifecycle
store/        bytdb scene persistence
tutorial/     built-in piano lesson catalog (pure data, served as JSON)
web/          rweb server, handlers, SSE meters, element UI, styl styles
plugins/src/  example external plugin (flanger)
```

## Tests

```bash
go test ./...        # DSP sanity (EQ gain, compressor, gate, reverb), polysynth,
                     # scene round-trip, tutorial catalog invariants
```

Smoke-test programs (engine tone, plugin load, recording) are archived in
`arch_test_scripts/`; run any with `go run ./arch_test_scripts/<name>`.
