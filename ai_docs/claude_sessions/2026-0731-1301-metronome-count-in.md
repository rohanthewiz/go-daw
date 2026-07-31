# Session: Metronome and Tutorial Count-In

- **Session ID**: `2754a2e1-ab2a-48f3-a8ef-808a3a8fe01d`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-1243-bank-drum-kit-names.md`

## Goal

Add a metronome and a tutorial count-in. Framing from the prompt: the
tutorial's Listen mode already schedules timed notes in the browser, so a
click track is just an oscillator burst on the same clock — the browser owns
*when*, the engine owns *sound*.

## Architecture

```
browser (app.js)                          Go engine (audio thread)
┌──────────────────────────┐   POST       ┌────────────────────────────────┐
│ drift-corrected          │ /api/click   │ clickGen.pending (atomic mail) │
│ setTimeout chain         │ ───────────► │  Swap(0) → 30ms sine burst     │
│ (or tutorial count-in)   │  {accent}    │  mixed AFTER recorder tap      │
└──────────────────────────┘              └────────────────────────────────┘
```

Key decision: the click renders **after** the recorder tap in
`processBlock` (new step 7), so it is heard in the monitor path but never
printed into a bounce — standard DAW behavior. Because that point is past
the master limiter, the click clamps its own sum (`clampUnit`).

## Changes by layer

### `audio/click.go` (new)

- `clickGen`: one-shot sine burst, 30ms exponential decay to −60dB.
  Beat = D6 (1175Hz) at 0.30 amp; accent (bar start) = A6 (1760Hz) at 0.45.
- Realtime contract: `pending atomic.Int32` mailbox (0 none / 1 tick /
  2 accent) — control plane `Store`s, callback `Swap(0)`s; all other fields
  audio-thread-owned. No locks, no allocation in the callback.
- Retrigger just restarts the burst; at any musical tempo bursts never
  overlap so coalescing within one block is harmless.

### `audio/engine.go`

- New `click *clickGen` field, built in `NewEngine` from the sample rate.
- `TriggerClick(accent bool)` — wait-free trigger for the HTTP layer.
- `processBlock`: step 7 `e.click.Render(out, n)` between the recorder tap
  and the device copy; steps renumbered 7→8.

### `web/handlers.go` + `web/server.go`

- `POST /api/click {accent}` → `clickHandler` → `TriggerClick`. A discrete
  event route like note/pedal (not a debounced param): every beat matters.

### `web/ui/transport.go`

- Metronome cluster between REC readout and scene box: `◆ CLICK` toggle
  button (`#metro-btn`), BPM number input (`#metro-bpm`, 30–300, default
  120), meter select (`#metro-beats`: 2/4, 3/4, 4/4 default, 6/8). Plain
  ids, no `data-param`, so the generic dispatcher ignores them.

### `web/assets/app.js`

- **Metronome section** (top level, independent of the piano block):
  setTimeout chain re-anchored against `performance.now()` each beat
  (`metroNext += interval`), so jitter never accumulates into tempo drift.
  BPM/meter are re-read every beat — edits apply within a beat, no restart.
  The button flashes per beat (`data-beat` 1 = accent bright amber, 2 =
  dim) as a silent visual metronome.
- **Count-in** (inside the tutorial block): `countIn(4, ms, done)` posts 4
  clicks (first accented), shows "Count-in… 4/3/2/1" in `tut-msg`. Pace =
  lesson's first-step ms clamped to [300, 700] (`countInMs`), so melodies
  count at 400ms and chord lessons don't crawl at 900ms.
  - `startLesson`: guides/checker arm only after the count-in completes.
  - `playDemo`: notes schedule after the count-in; `demoOn` spans the
    count-in so stray play stays out of the checker.
  - Count-in timers ride `demoTimers`, so Stop / lesson-switch cancels a
    pending count-in exactly like a demo.

### `web/styles/main.styl`

- `.metro-box`, `.metro-btn` (amber when on; two flash strengths placed
  after the `data-on` rule so the flash wins), `#metro-bpm`, `.metro-label`.

## Verification

- `go build ./...`, `go vet` (audio, web, web/ui), `node --check app.js` —
  clean.
- Stylus compiles with the new selectors (checked via a throwaway test in
  `web/` that ran `styl.Compile` on the embedded source, then was removed).
- Full `go test ./...` green.

## Possible next steps

- Make Start's count-in optional (it's a "ready-set-go" cue, not a tempo
  contract, since lessons are self-paced) — a two-line change in
  `startLesson`.
- Tap-tempo on the CLICK button; persist BPM in localStorage.
- Record count-in: N clicks before `record/start` engages, mirroring the
  tutorial flow.
- If sample-accurate click spacing ever matters (e.g. recording along at
  high tempo), move the beat clock into the engine (samples-per-beat
  counter in the callback) and let the browser only start/stop it.
