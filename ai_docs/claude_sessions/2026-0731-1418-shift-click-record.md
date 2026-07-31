# Session: Shift-Click Record (Count-In Bypass)

- **Session ID**: `72a1c948-6d7f-4022-89c8-d703bdbfc2c1`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-1333-record-count-in.md`

## Goal

The first next-step from the record count-in session: shift-click REC to
bypass the one-bar count-in — instant record, e.g. to capture something
already playing.

## Behavior

- **Shift-click REC while idle** → posts `record/start` immediately; no
  clicks, no countdown text.
- **Shift-click REC during a count-in** → cancels the remaining beats
  and starts recording right away (a plain click mid-count still just
  aborts).
- **Any click while recording** → stop, unchanged.

## Implementation (`web/assets/app.js`)

- In the `rec-btn` branch of the generic click dispatcher:
  - The mid-count guard now cancels the count-in and only `return`s when
    `!e.shiftKey`, so a shift-click falls through to the start logic.
  - A new `else if (e.shiftKey)` arm posts `record/start` directly and
    lights the button, skipping the count-in scheduling entirely.

## Implementation (`web/ui/transport.go`)

- REC tooltip now reads: "Record · starts after a one-bar count-in
  (press again to abort, shift-click to skip the count-in)".

## Verification

- `node --check app.js`, `go build ./...`, `go vet ./web/...` — clean.
- Pre-existing, unrelated: `unusedparams` lint note at `controls.go:88`.

## Possible next steps

- Persist metronome BPM (localStorage or a settings row in bytdb).
- Richer per-lesson stats readout while playing (last vs best).
- Optional count-in on tutorial Start.
