# Session: Record Count-In

- **Session ID**: `8325a31b-a26d-431b-b305-7c8506281d92`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-1316-tap-tempo-tutorial-progress.md`

## Goal

The remaining next-step from the metronome session: a one-bar count-in
before `record/start` engages, mirroring the tutorial's count-in flow so
the pace is in the player's ear before the take begins.

## Behavior

- **REC while idle** → one bar of clicks at the metronome's current
  tempo and meter (`metroIntervalMs()` + `#metro-beats`, so a 3/4 bar
  counts 3). First beat accented (`/api/click {accent:true}`), matching
  the metronome/tutorial voicing. The `#rec-time` readout counts down
  "in 4 … in 1"; when the bar completes, `record/start` posts and the
  button lights.
- **REC during the count-in** → aborts; the take never starts.
- **Metronome already running** → count-in is visual-only (no click
  posts): the metronome owns the click stream, a second phase-shifted
  stream would smear the beat.
- **REC while recording** → stop, unchanged.

## Implementation (`web/assets/app.js`)

- `recCountTimers` array at IIFE top level near the click dispatcher; a
  non-empty array doubles as the "counting in" flag. `cancelRecCountIn()`
  clears timers + readout.
- The `rec-btn` branch of the generic click dispatcher gained the
  count-in scheduling (per-beat closures over `i`, then a final timer
  that empties the array and posts `record/start`).
- The SSE meters handler (which repaints `#rec-time` at ~12.5Hz and
  still reports not-recording during a count-in) now skips blanking the
  readout while `recCountTimers.length` is non-zero.
- Scope note: the whole file is one IIFE, so the dispatcher can call
  `metroIntervalMs()` (hoisted function) and read `metroOn` (var,
  assigned at script eval) even though they're defined further down.

## Implementation (`web/ui/transport.go`)

- REC button tooltip: "Record · starts after a one-bar count-in (press
  again to abort)".

## Verification

- `node --check app.js`, `go build ./...`, `go vet ./web/...` — clean.

## Possible next steps

- Shift-click REC to bypass the count-in (instant record, e.g. to
  capture something already playing).
- Persist metronome BPM (localStorage or a settings row in bytdb).
- Richer per-lesson stats readout while playing (last vs best).
- Optional count-in on tutorial Start.
