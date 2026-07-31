# Session: Tap Tempo + Tutorial Progress Persistence

- **Session ID**: `8325a31b-a26d-431b-b305-7c8506281d92`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-1301-metronome-count-in.md`

## Goal

Two next-steps from the previous session: (1) tap tempo on the metronome
CLICK button, (2) tutorial progress persistence in bytdb, reusing the
scenes-store pattern. Also confirmed bytdb is already at v0.7.0 (no change
needed).

## Part 1 — Tap tempo (committed as `ad3eb46`)

The CLICK button does double duty; no second button was added.

- Every press is timestamped into `tapTimes` (capped at 5 → averages the
  last 4 gaps so mid-run tempo corrections take hold quickly).
- Presses 1–2 toggle on/off as before. Two toggles net back to the
  starting state, so a tap run begins from whatever state the user was in.
- A **3rd press within the 2s window** confirms a tap run: from then on
  each press sets `#metro-bpm` from the mean gap `(last−first)/(n−1)`,
  clamped 30–300 (the 2s window itself matches the 30bpm floor).
  Requiring three presses keeps an accidental double-toggle from
  rewriting the tempo.
- If the metronome is running while tapping, it **re-phases**: timer is
  cleared and `metroNext = lastTap + interval`, so the click falls in
  step with the hand instead of keeping its old phase at the new tempo.
- A pause > 2s ends the run; next press is a plain toggle.
- Toggle logic refactored into `metroStart()`/`metroStop()` helpers.
- Tooltip in `web/ui/transport.go`: "Metronome on/off · tap 3+ times to
  set tempo".

## Part 2 — Tutorial progress in bytdb (uncommitted at doc time)

### `store/progress.go` (new)

- Table `tutorial_progress`: `lesson TEXT PRIMARY KEY, completions INT,
  best_misses INT, last_misses INT, updated_at INT`.
- Keyed by lesson **name**, not catalog index — the compiled-in catalog
  may be reordered/extended; names are the stable identity.
- Plain columns, not a JSON blob (unlike scenes): the row shape is fixed
  counters, so no schema-churn concern and it stays queryable.
- `RecordLessonPass(lesson, misses)`: read-modify-write in Go
  (completions+1, lifetime-min best), keeping SQL to the same
  param-only `INSERT … ON CONFLICT DO UPDATE SET col = $n` shape scenes
  already proved works in bytdb — no bet on arithmetic in DO UPDATE.
  Single-writer server makes the RMW race theoretical.
- `ListProgress()` returns all rows, most recent first.
- `migrateProgress()` uses the same `information_schema.tables` probe as
  scenes (bytdb has no CREATE TABLE IF NOT EXISTS); called from `Open`
  in `store/scenes.go` after the scenes migration.

### API (`web/handlers.go`, `web/server.go`)

- `POST /api/tutorial/pass {lesson, misses}` → `RecordLessonPass`.
  Discrete-event route (like note/click); validates non-empty name and
  non-negative misses; logs the pass.
- `GET /api/tutorial/progress` → full list as JSON
  (`{lesson, completions, bestMisses, lastMisses, updatedAt}`).

### Client (`web/assets/app.js`)

- `tutProgress` map (lesson name → record), seeded from
  `/api/tutorial/progress` at page load. Catalog and progress load as
  independent fetches; each calls `markLessonOptions()` +
  `previewLesson()`, so arrival order never matters.
- On lesson completion (`tutAdvance` end branch): fire-and-forget POST
  of `{lesson: name, misses: tutMisses}` plus a local mirror update, so
  the ✓ and stats line update with zero round-trip wait.
- `markLessonOptions()`: appends " ✓" to completed lessons' options in
  the `#tut-lesson` picker (option value = catalog index → lesson name).
- `progressNote(lesson)`: preview line suffix, e.g.
  "Completed 3× · best: flawless." (best 0 renders as "flawless").

### Test (`store/progress_test.go`)

Runs 5→1→3 misses on one lesson + a flawless pass on another; asserts
completions=3 / best=1 / last=3, persistence across reopen (migration
probe on existing table), and non-zero updated_at.

## Verification

- `go build ./...`, `go vet ./...`, `node --check app.js` — clean.
- `go test ./store ./tutorial` — green (full suite green earlier).

## Possible next steps

- Persist metronome BPM (localStorage or a settings row in bytdb).
- Record count-in: N clicks before `record/start` engages.
- Show per-lesson stats richer than one line (e.g. last vs best in the
  progress readout while playing).
- Optional count-in on Start (two-line change in `startLesson`).
