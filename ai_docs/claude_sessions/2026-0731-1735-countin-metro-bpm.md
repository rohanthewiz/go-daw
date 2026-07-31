# Session: Tutorial Count-In Paced by Metronome BPM

- **Session ID**: `b9358b34-3e88-400b-af2d-515430524f4f`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-1728-tutorial-countin-toggle.md`

## Goal

Next-step from the count-in toggle session: pace the tutorial count-in
from the metronome's BPM instead of the lesson's first-step duration,
so one tempo knob governs every count-in on the page.

## Design

- **`metroIntervalMs()` as the single pace source** — the same clamped
  30–300 BPM mapping the metronome tick and the record count-in already
  use. Function declarations hoist through the IIFE, so the tutorial
  block (which sits earlier in the file) calls it safely at click time.
- **Signature folded to `countIn(n, done)`**: both call sites (Start
  and Listen) would have passed the identical `metroIntervalMs()`
  expression, so the interval is read inside the gate instead.
- **Old lesson-derived formula kept as a reference comment** in
  `countIn()` (clamped first-step duration), not deleted outright.
- **Carried over the record count-in's metronome rule**: when the
  metronome is running it owns the click stream — the count-in skips
  posting its own clicks (`if (!metroOn)`) and the "Count-in… N" text
  still paces the bar. This matters more now that both streams share a
  tempo; a phase-shifted duplicate would smear the beat.
- **Deliberately unclamped at the extremes** (30 BPM → 2s gaps,
  300 BPM → 200ms): the premise is that the tempo the player set is the
  tempo they expect to be counted in at.

## Implementation

- `web/assets/app.js`: removed `countInMs()`; `countIn()` reads
  `metroIntervalMs()` and gates clicks on `!metroOn`; both call sites
  updated to `countIn(4, done)`.
- `web/ui/tutorial.go`: checkbox tooltip now reads "…at the metronome
  tempo…".

## Verification

- `go build ./...`, `go vet ./web/...`, `node --check app.js` — all
  clean; grep confirms no `countInMs` references remain.
- Pre-existing, unrelated: `unusedparams` lint note at `controls.go:88`.

## Possible next steps

- Count one bar instead of a fixed four clicks: honor the metronome's
  beats-per-bar selector (the record count-in already does), updating
  the tooltip's "four pacing clicks" wording to match.
- Richer per-lesson stats readout while playing (last vs best).
- Split the toggle if desired: Listen keeps its count-in while Start
  obeys the setting — one-line change at the `countIn()` gate.
