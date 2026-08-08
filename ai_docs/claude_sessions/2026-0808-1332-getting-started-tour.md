# Session: Getting-started tour

- **Session ID**: `a281e1fe-5116-46dd-90d0-c3f58d641c4a`
- **Date**: 2026-08-08 13:32
- **Branch**: `main`

## What was asked

"Create an extensive start tour that shows how to get started with go-daw."
Clarified up front: an **in-app guided tour** (spotlight overlay over the live
console), not a prose GETTING_STARTED.md.

## What was built

A 34-step guided walk over the real mixer page, launched by **◈ Tour** in the
transport bar and auto-opened on a first run.

### 1. `tour/steps.go` — the catalog as pure data

Mirrors `tutorial/lessons.go` exactly: compiled-in data, served as JSON, never
touches the audio engine. A `Step` carries `Section`, `Target` (CSS selector),
`Title`, `Body`, optional `Tip`, `Place` (balloon side hint), and `Needs`.

Sections, in the order a first-time reader should meet the app: Welcome (what
it is + the signal path) → Transport (REC/count-in, metronome, tap tempo,
tempo+meter, scenes, meter stream) → Channel strip, walked top to bottom on
channel 1 (source types, gate, filters, EQ, comp, reverb, modules, pan, bus,
mute, fader+meter) → Buses (groups, master+limiter) → Piano (panel, channel
targeting, the four input routes, Web MIDI) → Lessons (catalog, Listen vs
Start, the chip strip) → Next (a suggested first session).

**Capability filtering** lives here as `Filter(Available{...})`. `Needs` values
are `duplex` / `soundbank` / `midi` / `groups`; the web layer fills `Available`
from the engine's duplex flag, console geometry, and the same directory scans
the source selectors already use. Filtering server-side keeps the checks next
to the state that answers them, and the client never reasons about why a step
should be hidden. An unknown `Needs` is returned as an error rather than
silently dropping the step; the handler logs it and falls back to the full
catalog (a tour with one extra step beats no tour).

On this machine (playback-only, no `.sf2` on disk) it correctly served **31 of
34** steps, dropping live-input, SoundFont, and MIDI-song.

### 2. `tour/steps_test.go` — catalog invariants

Same spirit as `lessons_test.go` — pin what the client assumes:

- every step has section/title/body; `Place` is a known value; `Place` is never
  set on a targetless step
- targetless cards (which render centered, no spotlight) only appear in the
  Welcome/Next sections — one buried mid-tour would read as a spotlight that
  failed to land
- sections are **contiguous**, since the balloon header labels runs of steps
- `Filter` with everything available returns the whole catalog; with nothing
  available returns exactly the unconditional steps
- an unknown `Needs` is rejected
- strip-walk steps stay on channel 1 (SoundFont step is the documented
  exception — it points at the channel `main.go` installs a sampled instrument
  on), so the spotlight never jumps sideways mid-section

### 3. `web/ui/tour.go` + transport button + page mount

Server renders only the shell (ring + balloon), TutorialPanel-style: all the
step text arrives from `/api/tour` and every position is view state only the
DOM can compute. `TourOverlay.Seen` renders server-side like the tutorial's
count-in checkbox, so the auto-start decision is made before paint and the tour
never flashes open on a return visit.

Mounted last in `<body>` so it stacks above every panel it spotlights without
bidding on z-index. Button sits next to the brand — on a first launch it should
be the easiest control to find.

### 4. `data-sect` on the channel strip's `<details>`

`gate` / `filters` / `eq` / `comp` / `reverb` / `modules`. Tour selectors then
name a section by *what it is* rather than by `nth-of-type` position, so
reordering the chain can't silently re-aim a spotlight at the wrong controls.

### 5. `/api/tour` + the `tour.seen` setting

One new route and one new entry in `settingValidators` (`"0"`/`"1"`, same shape
as `tut.countin`). Defaults to `"0"`, so a fresh profile is a first visit.

### 6. `web/assets/app.js` — the driver

## Two design decisions that shaped the rest

**The overlay does not block the page.** The dim is one element's outward
`box-shadow` (9999px spread) with `pointer-events: none`, so the spotlit
control stays clickable and the "try it now" tips are honest — you can actually
raise the oscillator level while reading about it.

**Because it doesn't block, the reader can trigger a structural change**
(swap a source, add a module) that reloads the page mid-tour. The step index
rides `sessionStorage`, so the tour reopens exactly where it left off instead
of punishing curiosity. This is what makes the non-blocking choice viable
against the page's existing "structural change → reload" architecture.

Supporting behavior:

- **Skip-if-missing/hidden**: `tourGo(i, dir)` searches onward in the direction
  of travel past any step whose selector misses or whose element has zero
  height. Off the end forward ends the tour; off the start backward re-enters
  from the top.
- **Collapsed sections**: ancestors are opened before measuring (a control in a
  folded `<details>` reports zero height and would look hidden), tracked, and
  closed again on leaving — a walk down the strip doesn't leave six sections
  sprung open behind it.
- **Balloon placement**: tries the step's preferred side, then below/right/
  above/left, taking the first that fits the viewport whole on its placement
  axis; the cross axis is clamped. Falls back to preferred-and-clamped.
- **Reposition on scroll (capture, so the horizontally-scrolling channel rack
  counts) and resize**; no-op while closed.
- **Keyboard**: ←/→ page, Esc leaves. Skipped while an INPUT/SELECT/TEXTAREA
  has focus, so arrows still nudge the slider you're holding.
- Dots double as jump targets — on a 31-step tour, "back to the piano" should
  not mean twelve presses of Back.

## Verification done

`go build ./...`, `go vet ./...`, `go test ./...` all clean. Then drove the
real app in Chrome (`-no-input -addr :8123`) and confirmed each behavior:

- auto-opens on first visit, centered welcome card, page dimmed
- ring lands on `#rec-btn`, `.metro-box`, etc.; balloon flips sides sensibly
- folded Gate section auto-opens when spotlit, and is **closed again** after
  moving on (verified via JS: `gateStillOpen: false`)
- scrolls down to the piano and places the balloon above it
- final step's Next reads "Done"; finishing persists `tour.seen=1` and a reload
  no longer auto-opens
- **the resume path**: at step 10 (Src), switched channel 1 `osc → synth` →
  page reloaded → tour came back on step 10/31, and the next press correctly
  **skipped** step 11 (the now-hidden `.src-osc` row) straight to 12/31
- no console errors throughout

**One CSS bug found and fixed by looking**: `.tour-foot button` out-specified
`.tour-dot`, stretching every progress dot into a 30px bar. Scoped to
`.tour-foot > button` (dots live inside `.tour-dots`, not as direct children).

Cleanup: reset `tour.seen` back to `"0"` afterward so the first-run auto-open
still happens for the user. `godaw.db*` is gitignored, so nothing leaked.

## Notes / possible next steps

- The tour is the third thing to use the "server owns pure-data catalog, client
  owns all interaction" pattern (after lessons and the GM program list). That
  boundary is holding up well.
- `tour.seen` is a single global flag. If the tour ever gains chapters worth
  re-running independently, it'd want per-section state — but a 31-step read is
  a one-time thing, so one flag is right for now.
- Possible follow-up: a "skip to section" jump in the balloon head (the dots
  already allow arbitrary jumps, but by position, not by name).
- Possible follow-up: steps that *demonstrate* rather than describe — e.g. the
  metronome step could start the click and stop it on leaving. Deliberately not
  done: a tour that changes audio state behind the reader's back is a worse
  default than one that invites them to press the button themselves.
