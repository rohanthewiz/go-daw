# Session: Drum Kit Names From the Bank's Own Preset List

- **Session ID**: `fa265a97-a601-409a-bd06-7bbffc311c64`
- **Date**: 2026-07-31
- **Branch**: main
- **Continues**: `2026-0731-0842-drum-kit-selector.md` (its first "possible next step")

## Goal

Replace the fixed nine-entry GS drum-kit table in the SoundFont kit selector
with the kits the loaded bank actually contains, labeled with the bank
author's own preset names.

## Why it matters (measured against the repo's three banks)

| | GS table | GeneralUser-GS | FluidR3_GM | MuseScore_General |
|---|---|---|---|---|
| kits | 9 | 13 | 31 | 37 |
| kit 25 | TR-808 | **808/909** | TR-808 | TR-808 |
| extras | — | Dance (26), CM-64/32L (127) | Standard 1-7, Room 1-7 | + Marching Snare (56) |

The table was both wrong (kit 25's real name in GeneralUser-GS) and lossy
(hiding 4-28 kits per bank). Bank names also already read as kit names
("Orchestra Kit", "Marching Snare"), so the old `" Kit"` suffix had to go —
it would have rendered "Orchestra Kit Kit".

## Changes by layer

### `audio/source/soundfont.go`

- `sfDrumBank = 128` named for the SF2 bank meltysynth pins channel 9 to.
- `BankPreset{Program, Name}` + `BankDrumKits(path) ([]BankPreset, error)`:
  scans `sf.Presets` for `BankNumber == 128`, trims names (SF2 stores them in
  fixed-width fields, so trailing pad chars are common), sorts by program
  (file order is authoring-tool dependent, program order is what a player
  scrolls through).
- Reads through the **existing process-wide `bankCache`**, so for a bank a
  channel already plays this is a map lookup plus a scan of a few hundred
  preset headers — cheap enough for every page render. Documented that
  passing an unloaded path would pay the full 100-200 MB parse.

### `web/ui/gm.go`

- `gmDrumKits` typed as `[]drumKitOption` and **demoted to fallback**.
- `drumKitOptions(bankPath, cur)`:
  - prefers the bank's presets; falls back to the GS table when the bank is
    unreadable or declares no percussion presets — an empty dropdown would
    strand a player in drums mode with no way out, and GS entries still work
    there since a missing preset degrades to the synth's default.
  - **Placeholder for `cur` not in the bank** — the realistic case right after
    a bank switch (GeneralUser's kit 26 has no counterpart elsewhere). Inserts
    `N · (not in bank — Standard)` *in program order* via `sort.Search`, so
    the select keeps showing the server's true state instead of silently
    snapping to another kit and lying about what's playing.
  - `logger.LogErr(serr.Wrap(...))` on the read failure, per app conventions.

### `web/ui/channelstrip.go`

- Kit `<select>` options now come from `drumKitOptions(sfontPath, sfontKit)`.
- `" Kit"` suffix removed (see above).
- **No client change needed**: a bank switch already reloads the page, so the
  list rebuilds for the new bank. The JS bank-switch POST still carries
  `drumKit`, so the server restores it and the placeholder path covers the
  rest.

## Tests

### `audio/source/soundfont_test.go`

- `TestBankDrumKits` — bank-agnostic assertions (any GM bank has Standard at
  program 0 in bank 128), ascending order, trimmed non-empty names, and then
  **walks every listed kit asserting each produces audio** on note 38. That
  last part is the real claim: reading the bank means every offered entry is
  genuinely selectable.
- `TestBankDrumKitsMissing` — the error path the UI fallback depends on.

### `web/ui/gm_test.go` (new file — first test in `web/ui`)

- `testBank(t)` helper mirroring the source package's skip-if-absent
  convention.
- `TestDrumKitOptionsFallback` — `""` and a bogus path both yield the GS list.
- `TestDrumKitOptionsFromBank` — asserts the result **differs** from the GS
  fallback, which is what proves bank presets were actually read.
- `TestDrumKitOptionsPlaceholder` — kit 30 (between GS 25 and 32, defined by
  no bank here) appears in correct program order; a kit the bank *does* define
  is not duplicated.

## Verification

`go build ./... && go vet ./...` clean. `go test ./...` fully green; the new
tests confirmed running (not skipping) against the real
`soundbanks/GeneralUser-GS.sf2`.

## Possible next steps

- Same treatment for the melodic program select: list the bank's bank-0
  presets instead of the fixed GM 128 table (`BankDrumKits` generalizes to
  `BankPresets(path, bank)` for this).
- Per-kit key maps / labels on the virtual piano when drums mode is on.
- MIDI program-change passthrough from Web MIDI input to kit/program selects.
