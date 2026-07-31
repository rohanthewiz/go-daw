# Session: SoundFont (SF2) Sound Banks + SFSynth Source

- **Session ID**: `46dcdd84-2bf2-44ca-b883-5cf90bdfddb0`
- **Date**: 2026-07-31
- **Branch**: main

## Goal

Two-part session: (1) find and download quality MIDI sound banks compatible
with go-daw, (2) wire up a SoundFont playback source so the banks are actually
playable from the mixer, virtual piano, Web MIDI, and tutorial.

## Part 1 — Sound banks downloaded

SoundFont 2 (`.sf2`) chosen as the format: it's the de-facto GM bank standard
and `github.com/sinshu/go-meltysynth` gives a pure-Go render path (no new cgo).

Downloaded into `soundbanks/` (gitignored via `soundbanks/*.sf2`; README and
licenses are tracked):

| Bank | Size | Notes |
|------|------|-------|
| GeneralUser-GS.sf2 | 32 MB | best quality-to-size GM bank; free incl. commercial |
| FluidR3_GM.sf2 | 148 MB | classic full GM bank (FluidSynth default); MIT |
| MuseScore_General.sf2 | 216 MB | MuseScore's HQ default, FluidR3-derived; MIT |

Sources: MuseScore mirror `https://ftp.osuosl.org/pub/musescore/soundfont/`
and GitHub `mrbumpy409/GeneralUser-GS`. All verified as `RIFF...sfbk`.
Skipped: Sonatina Symphonic Orchestra (417 MB, orchestral-only — grab later if
needed from the same mirror).

## Part 2 — SFSynth source implementation

### New: `audio/source/soundfont.go`

- `SFSynth` implements `Source` + `NotePlayer`; renders via meltysynth.
- **Realtime contract verified**: meltysynth's voice pool and block buffers
  are fully allocated in `NewSynthesizer`; NoteOn/NoteOff/Render neither
  allocate nor lock (checked its source in the module cache). Enforced by
  `TestSFSynthReadNoAllocs`.
- Same lock-free event-ring discipline as PolySynth (producers under mutex,
  audio thread drains via atomics). Program changes ride the same ring as
  notes (bit 9 = `evProgram`) so ordering with notes holds.
- Bank cache: package-level `map[path]*meltysynth.SoundFont` — banks are
  immutable after parse, so N channels share one copy of a 100+ MB sample set
  (covered by `TestSFSynthBankCache`).
- Velocity mapping `int32(vel*126)+1` so near-zero velocities don't become
  MIDI 0 (which meltysynth treats as note-off).
- meltysynth reverb/chorus left enabled (GM banks are voiced expecting them);
  channel-strip reverb stacks on top. One-line change in `NewSFSynth` if too much.

### New: `source.NotePlayer` interface (`source.go`)

`NoteOn(note int, vel float64)` / `NoteOff(note int)` — piano handler, piano
UI panel, Web MIDI, and tutorial all target this instead of `*PolySynth`, so
any playable source works with zero handler changes.

### Touched files

- `mixer/state.go` — `SourceState` gains `Program`; new `"sfont"` case in
  `SetChannelSource` (default level −6 dB; zero = unset convention) and
  `captureSource` (persists path + program in scenes).
- `config/config.go` — `SoundbanksDir` (`soundbanksDir`, default `soundbanks`).
- `web/handlers.go` — `sfont.level` / `sfont.program` source params (program
  change is live, no rebuild); `noteHandler` now checks `source.NotePlayer`;
  `listSoundbanks()` scan + `GET /api/soundfonts`.
- `web/server.go` — route for `/api/soundfonts`.
- `web/ui/channelstrip.go` — `sfont` option (disabled when no banks on disk),
  bank + GM-program selects, level slider.
- `web/ui/gm.go` (new) — 128-name GM Level 1 program table + `sfontBaseName`.
- `web/ui/page.go` — `PageData.Soundbanks` threaded to strips.
- `web/ui/piano.go` — playable-channel detection via `NotePlayer`.
- `web/assets/app.js` — `.src-sfont` row visibility; sfont install sends
  `{type, path, program}`; `sfont-bank` change rebuilds source (reload);
  `sfont-program` change posts live source-param.
- `web/styles/main.styl` — `.src-sfont` show/hide + stacked full-width selects.
- `main.go` — channel 4 defaults to "SF Piano" via `defaultSoundbank()`
  (prefers GeneralUser GS, else first `.sf2`, else silently dormant).
- `soundbanks/README.md` — bank docs + integration notes.
- `go.mod/go.sum` — added `github.com/sinshu/go-meltysynth`.

### Key decisions

- **Bank switch = structural rebuild + page reload; program switch = live**
  (event ring, sounding notes unaffected). The two selects carry different
  data-roles for this reason.
- Bank scan happens per page render (no cache) so dropping a new .sf2 shows
  up on reload without restart.
- Plugins rebuilt after go.mod change (toolchain-identity rule):
  `go build -buildmode=plugin -o plugins/flanger.so ./plugins/src/flanger`.

## Verification (live app on :8000)

- `GET /api/soundfonts` → all 3 banks listed.
- Channel 4 came up as "SF Piano" with GeneralUser-GS.
- C-major chord on ch 4 → 200s; meter attack peak ≈ −25 dBFS (real signal).
- Live program change to 48 (String Ensemble 1) → 200, audible, snapshot
  shows `program: 48`.
- Bank switch to FluidR3 + program 24 → clean rebuild, snapshot correct.
- Error paths: unknown source param → 400; note to non-synth channel → 409.
- `go build ./... && go vet ./... && go test ./...` all green; SFSynth tests
  skip gracefully when no bank on disk (CI-safe).

## Follow-up ideas

- Percussion: GM drums live on MIDI channel 9 with the percussion bank; the
  ring/drain currently drives channel 0 only — a "drums" toggle could map to
  channel 9.
- MIDI file playback via `meltysynth.MidiFileSequencer` as another source.
- Expose meltysynth reverb/chorus enable as a source param if doubling with
  strip reverb bothers mixes.
