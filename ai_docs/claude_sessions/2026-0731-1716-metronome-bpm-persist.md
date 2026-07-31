# Session: Persist Metronome BPM in bytdb

- **Session ID**: `258e3a37-6efe-410b-8906-bf2fef5bd14b`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-1418-shift-click-record.md`

## Goal

First next-step from the shift-click record session: persist the metronome
BPM in bytdb so the tempo survives a server restart.

## Design

- **Generic `settings` table** rather than a metronome-specific one:
  `key TEXT PRIMARY KEY, value TEXT, updated_at INT`. Values are TEXT even
  for numbers — settings are read one at a time by name, never aggregated
  in SQL, so the table never migrates when a future setting isn't numeric.
- Defaults live in code, not the table: `GetSetting` returns
  `(value, found)`; absence means "use the default" (120), not an error.
- **Whitelisted endpoint**: `POST /api/setting {key, value}` validates
  against `settingValidators` — `metro.bpm` must be an integer in 30–300.
  A new persisted preference = one validator entry + one client save call.
- **Server-rendered value**: the page handler reads the saved BPM and
  threads it `PageData → TransportBar → value` attr of the BPM input, so
  the tempo is correct at first paint — no fetch, no default-then-settle
  flash.

## Implementation

- `store/settings.go` (new): `migrateSettings` (same catalog-probe pattern
  as scenes/progress — bytdb has no `CREATE TABLE IF NOT EXISTS`),
  `SetSetting` upsert, `GetSetting`. Wired into `Open` in
  `store/scenes.go`.
- `store/settings_test.go` (new): absent → set → overwrite → reopen
  round-trip.
- `web/handlers.go`: `settingHandler` + `settingValidators` whitelist;
  `metroBPM()` helper reads the setting for page render, defaulting to
  "120" on absence or store error.
- `web/server.go`: `api.Post("/setting", srv.settingHandler)`.
- `web/ui/page.go`, `web/ui/transport.go`: `MetroBPM string` threaded
  through; input renders the persisted value.
- `web/assets/app.js`: `saveMetroBpm()` with its own 400ms trailing
  debounce (not the shared 30ms param window — tap tempo rewrites the
  value ~2×/sec; one write lands after the run settles). Fires on the
  input's `change` event (commit-only: Enter/blur/spinner) and is called
  explicitly after a confirmed tap-tempo run, since programmatic value
  writes emit no `change` event.

## Verification

- `go build ./...`, `go vet ./web/... ./store/...`, `go test ./store/`
  (both round-trip tests pass), `node --check app.js` — all clean.
- Pre-existing, unrelated: `unusedparams` lint note at `controls.go:88`.

## Possible next steps

- Persist beats-per-bar: add a `metro.beats` validator entry + a save call
  on the selector, and render the selected option server-side.
- Richer per-lesson stats readout while playing (last vs best).
- Optional count-in on tutorial Start.
