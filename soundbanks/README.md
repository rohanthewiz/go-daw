# Sound Banks

General MIDI SoundFont (.sf2) banks for go-daw. SF2 is the de-facto standard
MIDI sound-bank format: one file bundles samples + key/velocity mappings +
envelopes for all 128 GM instruments plus percussion.

| File | Size | What it is | License |
|------|------|-----------|---------|
| `GeneralUser-GS.sf2` | ~31 MB | S. Christian Collins' GeneralUser GS v2.0.3 — widely considered the best quality-to-size GM bank | Free, incl. commercial (see `GeneralUser-GS_LICENSE.txt`) |
| `FluidR3_GM.sf2` | ~141 MB | Frank Wen's FluidR3 — the classic full-size GM bank (FluidSynth's default) | MIT |
| `MuseScore_General.sf2` | ~216 MB | MuseScore's default HQ bank (FluidR3-derived, many improved instruments) | MIT (see `MuseScore_General_License.md`) |

Sources:
- GeneralUser GS: https://github.com/mrbumpy409/GeneralUser-GS
- FluidR3 / MuseScore_General: https://ftp.osuosl.org/pub/musescore/soundfont/

Also available at the MuseScore mirror but not downloaded (417 MB):
`Sonatina_Symphonic_Orchestra_SF2.zip` — orchestral-only bank, grab it if we
add orchestral scoring features.

## Playback in go-daw

Wired up via the `sfont` channel source (`audio/source/soundfont.go`), which
renders through `github.com/sinshu/go-meltysynth` — a pure-Go SoundFont
synthesizer whose render path is allocation- and lock-free after init, so it
honors the engine's realtime contract.

- Any `.sf2` dropped in this folder appears in each channel strip's `sfont`
  bank selector on the next page load (dir is scanned per render; also served
  at `GET /api/soundfonts`). Configure the folder with `soundbanksDir`.
- Instrument (GM program 0..127) switches live via the synth's event ring;
  bank switches rebuild the source. Parsed banks are cached process-wide, so
  channels sharing a bank share one in-memory copy.
- On startup, channel 4 becomes "SF Piano" using GeneralUser GS when present
  (else the first bank found). The virtual piano, Web MIDI input, and tutorial
  all drive it through the same NotePlayer interface as the additive synth.
