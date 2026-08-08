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
- **Getting-started tour**: a 34-step guided walk over the live console, served
  from `/api/tour` and launched by **◈ Tour** in the transport bar (it opens
  itself on a first run). Each step spotlights a real control by CSS selector,
  auto-opens the collapsed section it lives in, and dims everything else —
  without blocking the page, so "try it now" tips are actually doable. The step
  index rides `sessionStorage`, so changing a source mid-tour reloads the page
  and picks up exactly where you were, and steps whose target is missing or
  hidden are skipped. The server drops steps this instance can't demonstrate
  (live input on a playback-only run, SoundFonts with no banks on disk).
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

That's the entire contract. Everything else — the UI dropdown entry,
auto-generated parameter sliders, chain insertion, and scene persistence —
comes for free from the `AudioModule` interface. There is no registration
code to write: the loader globs `plugins/*.so` at startup, looks up
`NewModule`, and registers the factory under the module's `Name()`.

### Walkthrough: the Bass Xpander (`plugins/src/bass_xpander/`)

A bass enhancer that adds tanh-generated harmonics and a flip-flop
sub-octave under a crossover frequency. It shows every part of the
contract in one file.

**1. Create the source under this repo** at `plugins/src/<name>/main.go`.
It must be `package main` (with an empty `func main()` so `go build ./...`
still passes) and live in this module — see the toolchain gotcha below.

**2. Hold parameters in atomic cells.** `Process` runs on the audio thread
while the web UI writes params from HTTP handlers, so back every parameter
with a `dsp.ParamCell` (a lock-free atomic float):

```go
type BassXpander struct {
	xover  *dsp.ParamCell // crossover Hz
	drive  *dsp.ParamCell // waveshaper gain
	amount *dsp.ParamCell // harmonics level
	sub    *dsp.ParamCell // sub-octave level
	// ... fixed-size DSP state (filter memories, envelope, flip-flop)
}

func NewModule() module.AudioModule {
	return &BassXpander{
		xover:  dsp.NewParam(150),
		drive:  dsp.NewParam(4),
		amount: dsp.NewParam(0.5),
		sub:    dsp.NewParam(0.4),
	}
}
```

**3. Implement the five interface methods:**

- `Name() string` — registry key and UI label (`"bass_xpander"`).
- `Init(sampleRate float64, maxBlock int) error` — called once on the
  control plane; do **all** allocation here, sized for `maxBlock`.
- `Process(l, r []float32)` — transform the block in place. Realtime
  rules: no allocation, locks, logging, or blocking. Read each ParamCell
  once per block, not per sample.
- `Params() []module.ParamSpec` — metadata the web UI turns into sliders
  (min/max/default/unit, `ScaleLog` for frequency-like knobs):

```go
func (b *BassXpander) Params() []module.ParamSpec {
	return []module.ParamSpec{
		{ID: "xover", Name: "Crossover", Unit: "Hz", Min: 50, Max: 400, Default: 150, Scale: module.ScaleLog},
		{ID: "drive", Name: "Drive", Min: 1, Max: 10, Default: 4},
		{ID: "amount", Name: "Harmonics", Min: 0, Max: 1, Default: 0.5},
		{ID: "sub", Name: "Sub Level", Min: 0, Max: 1, Default: 0.4},
	}
}
```

- `SetParam(id, value)` / `GetParam(id)` — switch on the param ID and
  forward to the matching ParamCell.

**4. Build the `.so`** (from the repo root, same toolchain as the binary):

```bash
go build -buildmode=plugin -o plugins/bass_xpander.so ./plugins/src/bass_xpander
```

**5. Restart `godaw`.** The startup log shows
`Loaded plugin module name=bass_xpander`, and the module appears in every
channel's module dropdown with its four sliders. To verify outside the
app, `go run ./arch_test_scripts/plugin_smoke` loads all plugins and runs
a block through each.

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
tour/         built-in getting-started tour (pure data, served as JSON)
tutorial/     built-in piano lesson catalog (pure data, served as JSON)
web/          rweb server, handlers, SSE meters, element UI, styl styles
plugins/src/  example external plugins (flanger, bass_xpander, cross_fader)
```

## Tests

```bash
go test ./...        # DSP sanity (EQ gain, compressor, gate, reverb), polysynth,
                     # scene round-trip, tutorial and tour catalog invariants
```

Smoke-test programs (engine tone, plugin load, recording) are archived in
`arch_test_scripts/`; run any with `go run ./arch_test_scripts/<name>`.
