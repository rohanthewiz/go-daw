# Session: Piano Tutorial

- **Session ID**: `7cdf9738-d820-406c-a311-4b553c9305d0`
- **Date**: 2026-07-29 19:27
- **Branch**: main
- **Previous session**: [2026-0729-1545-web-midi.md](2026-0729-1545-web-midi.md)

## Goal

Add a guided piano tutorial facilitated through the existing platform: server-rendered shell, lessons as server data, and all interaction client-side over the same `noteOn`/`noteOff` plumbing the virtual piano already uses. No audio-engine, synth, or mixer changes were needed — the tutorial is a pure consumer of the existing note path.

## Key insight

Every input route (mouse/touch, computer keyboard, Web MIDI, and now demo playback) funnels through the two shared functions `noteOn`/`noteOff` in `web/assets/app.js`. Tapping those two functions gives the tutorial checker visibility into *all* play for free, and lets Listen mode replay lessons through the real synth via the existing `POST /api/channel/:id/note`.

## What was built

### 1. `tutorial/` package (new) — `tutorial/lessons.go`

Pure-data lesson catalog, zero runtime state:
- `Step{Notes []int, Label string, Ms int}` — notes that must be held together (1 = melody, n = chord), strip label, demo duration.
- `Lesson{Name, Desc, Steps}`; `Lessons()` returns the shared read-only slice.
- Authoring helpers: `midi("C4")` → 60 parser (panics on malformed names at init — compiled-in data, fail fast), `note(name, ms)` (label drops octave digit), `chord(label, ms, names...)`, and `melody("C4 D4 E4- ...")` — space-separated pitches, trailing `-` = double length (800ms vs 400ms).
- 7 lessons, easiest first: First Steps, C Major Scale, Mary Had a Little Lamb, Twinkle Twinkle, Ode to Joy, First Chords (C-F-G-C triads), Pop Progression (I–V–vi–IV with Am).
- Authoring invariant: every lesson fits the 25-key window once base = the C at/below its lowest note (enforced by test).

### 2. `web/ui/tutorial.go` — TutorialPanel

Shell-only component under the piano (rendered in `page.go` after PianoPanel): lesson `<select>` (options carry catalog index), Start / Listen / Stop buttons, `#tut-progress`, `#tut-msg`, and empty `#tut-strip`. Client fills everything from JSON, mirroring the Web MIDI shell pattern.

### 3. API — `GET /api/lessons`

`lessonsHandler` in `web/handlers.go` serves `tutorial.Lessons()` verbatim; route added in `web/server.go`. Client fetches once at page load.

### 4. Client — tutorial block in `web/assets/app.js`

Inside the `if (pianoKeys)` closure (shares `noteOn`, `base`, `setBase`, `light`):
- **Hooks**: `noteOn`/`noteOff` now call `tutNoteOn`/`tutNoteOff` (function declarations hoist within the block, so order is safe).
- **Checker**: `tutHeld` map; wrong note → miss (red flash 250ms, counted); step completes when `step.notes.every(held)` — melodies are chords of one. Checking only on note-on means holding a common tone across two chords works like a real piano.
- **Guides**: `data-guide="1"` amber inset ring on target keys; `fitBase()` auto-shifts the octave so the whole lesson is visible; `setBase` re-aims guides if the user shifts octave mid-lesson.
- **Strip**: one chip per step; `data-done` (green) / `data-cur` (amber) cursor with `scrollIntoView({block:"nearest", inline:"center"})`.
- **Listen demo**: whole lesson scheduled with setTimeouts (display-grade timing); notes release 80ms early so repeats re-strike; `demoOn` flag keeps demo notes (and stray user input) out of the checker; `stopDemo()` clears timers and releases `demoSounding`. Listen deactivates checking; Start re-arms.
- **Completion**: "flawless!" or "N missed. Try again?".

### 5. Styles — `web/styles/main.styl`

`.tutorial` panel (amber `.tut-title`), `.tut-strip`/`.tut-chip` (horizontal scroll, no wrap — reads like a score), key cues: `.piano-key[data-guide="1"]` amber inset box-shadow (coexists with accent-blue pressed fill), `.piano-key.white|.black[data-miss="1"]` red — placed *after* the white/black blocks to win the specificity tie against `[data-on]`.

### 6. README

Documented the previously-undocumented virtual piano + Web MIDI, the new tutorial, the `tutorial/` layout entry, and the expanded test line.

## Verification

- `go build ./...`, `go vet ./...`, all tests green; `node --check` on app.js.
- New `tutorial/lessons_test.go`: pins pitch parsing anchors (C4=60, A4=69, F#3=54) and the catalog invariant (name/desc/steps present, every note within `base..base+24` after client fitBase).
- Scratchpad smoke check (`tutcheck/main.go`): page HTML contains all 8 tutorial element IDs + lesson names, catalog marshals (`"notes":[60,64,67]` present, ~4.4KB JSON), main.styl compiles with `.tut-chip` / guide rules. (`mixer.NewConsole(numChannels, numGroups int, sampleRate float64, maxBlock int)`.)
- **Not verified**: live browser play-through — `go run .`, open http://localhost:8000, pick a lesson, Start/Listen.

## Deferred / future ideas

- **Timing-scored lessons**: score rhythm accuracy against step `Ms` (currently only correctness is checked).
- **Metronome / count-in** for Listen and play modes.
- **Left-hand / two-hand lessons**: chords + melody simultaneously — checker already supports arbitrary note sets per step.
- **Per-user progress persistence** (bytdb): best miss counts per lesson, unlock ordering.
- **MIDI-out lesson recording**: capture a user's pass into a scene/WAV via the existing recorder.
