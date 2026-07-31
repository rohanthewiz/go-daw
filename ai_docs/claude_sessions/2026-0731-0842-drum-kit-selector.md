# Session: Drum Kit Selector (Channel 9 Kits)

- **Session ID**: `0fff6889-7c29-433c-8421-ec648949d785`
- **Date**: 2026-07-31
- **Branch**: main

## Goal

Add a drum kit selector for the SoundFont source's percussion mode: choosing
among the bank's channel-9 kits (Standard, Room, Power, TR-808, Jazz, Brush,
…) as a companion to the existing Drums toggle.

## Key insight (verified in meltysynth source)

A kit change is just a **MIDI program change on channel 9**. meltysynth pins
channel 9 to bank 128 (`channel.go`: percussion channels get
`bankNumber = 128`), so `ProcessMidiMessage(9, 0xC0, kit, 0)` selects among
percussion presets. Preset lookup (`synthesizer.go:225-240`) falls back to the
GM preset id and then the `defaultPreset` when a (bank, patch) pair is
missing — so **selecting a kit the loaded bank lacks degrades gracefully to
the Standard kit instead of going silent**. Every dropdown entry is safe to
offer regardless of bank.

## Changes by layer

### `audio/source/soundfont.go`

- New ring event `evDrumKit = 1 << 12`; low byte carries the kit's program
  number. Distinct from `evProgram` because the two target different channels
  (melodic ch 0 vs percussion ch 9 / bank 128).
- `SetDrumKit(kit int)` / `DrumKit() int` with an `atomic.Int32` display
  mirror — exactly the `SetProgram`/`Program` pattern. Out-of-range values
  (< 0 or > 127) are ignored, not clamped.
- Drain case sends `0xC0` on channel 9. **No note-off sweep** (unlike the
  drums toggle): sounding drum hits are one-shots that finish on the old
  kit's samples, matching hardware-module behavior.

### `web/ui/gm.go`

- `gmDrumKits`: sparse pair list (not a `[128]string`) of the nine GS
  conventional kits — 0 Standard, 8 Room, 16 Power, 24 Electronic, 25 TR-808,
  32 Jazz, 40 Brush, 48 Orchestra, 56 SFX. Sparse because listing 119 empty
  program slots would bury the real choices.

### `web/ui/channelstrip.go`

- Kit `<select data-role="sfont-kit">` in the sfont sub-controls, next to the
  Drums toggle. Always rendered (even with Drums off) — picking a kit before
  toggling drums on is a natural flow, and hiding it would make the feature
  undiscoverable.

### `web/handlers.go` + `web/assets/app.js`

- `sfont.kit` added to the live source-param route (like `sfont.program`) —
  rides the event ring, no rebuild/reload on change.
- **Adjacent gap fixed**: the JS bank-switch rebuild used to POST only
  `path` + `program`, silently resetting drums mode. It now also carries
  `drums` (from the toggle checkbox) and `drumKit` (from the select) — the
  `/source` endpoint already decodes a full `mixer.SourceState`, so no server
  change was needed for this.

### `mixer/state.go` (scene persistence)

- `DrumKit int` (`json:"drumKit,omitempty"`) on `SourceState`; captured in
  `captureSource`, restored in `SetChannelSource` when nonzero. Zero doubles
  as "unset" per the omitempty convention — harmless, since kit 0 (Standard)
  is also the synth's channel-9 default.

### Tests (`audio/source/soundfont_test.go`)

- `TestSFSynthDrumKitChange`: mirror state, out-of-range rejection, and
  audio-after-kit-change (guaranteed by the fallback behavior above).
- `SetDrumKit(8)` added to the `TestSFSynthReadNoAllocs` loop so the new
  drain path is held to the zero-alloc realtime contract.

## Verification

`go build ./... && go vet ./...` clean; full SFSynth suite passes against the
real `soundbanks/GeneralUser-GS.sf2` (tests skip gracefully where no bank
exists).

## Possible next steps

- Show kit names from the actual bank's preset list (bank 128 presets) rather
  than the fixed GS table, for non-GS banks with custom kits.
- Per-kit key maps / labels on the virtual piano when drums mode is on.
- MIDI program-change passthrough from Web MIDI input to kit/program selects.
