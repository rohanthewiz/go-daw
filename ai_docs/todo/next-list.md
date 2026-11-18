# go-daw — Next list

The project's one living list of follow-ups. Sessions edit this file in place
(`/sess-save` records what each session closed and raised as
`Closed: … Raised: …`) instead of copying a Next section forward, so an item
can only leave the list as a visible edit in git history.

Seeded 2026-11-18 (session `2026-1118-0719-reliability-pass`) by `/next-list seed` from all 19 earlier session docs
(`2026-0729-1413-go-daw-initial-build` … `2026-0811-2014-soundcraft-ui24r-console-restyle`).

## Conventions

- **IDs** (`N-001`…) are permanent and never reused or renumbered.
- **raised** is the session-doc stem (`ai_docs/claude_sessions/<stem>.md`) the
  item first appeared in.
- **Age** is computed by `/next-list` (session docs since `raised`), never
  stored here.
- **value** is the payoff of doing it, not the effort:
  - `high` — something is being worked around today, or a second independent
    consumer has arrived
  - `medium` — it blocks one named thing, or it is a visible defect nobody has
    to route around yet
  - `low` — a gap nobody has bumped into, or contingent on something that does
    not exist
- **Open** is the next build work (code, a defect, a decision). **Validate**
  is the next testing: run, look, hear, measure, or write or repair a test; no
  product change planned unless the check finds a defect. **Roadmap** is
  wanted but later. **Non-goals** is likely never.
- Nothing leaves Open, Validate or Roadmap without a line in another section
  (Closed, Non-goals, or a move between the three).
- Open, Validate and Roadmap each stay in ID order.

**Next ID:** N-050

## Open

- **N-002** · raised `2026-0729-1451-virtual-piano` · value low
  Velocity curve setting for the virtual piano / Web MIDI input (today:
  `velFromEvent` strike height and MIDI `v/127`, both linear).

- **N-003** · raised `2026-0729-1451-virtual-piano` · value low
  More waveform/timbre presets for `PolySynth` (still a fixed four-harmonic
  additive tone, `audio/source/polysynth.go`). Partly overtaken: the SoundFont
  source (`sfont`) now gives 128 GM programs, so this only matters for the
  zero-asset synth.

- **N-004** · raised `2026-0729-1451-virtual-piano` · value low
  Multi-client key highlighting via a new SSE event type (notes played in one
  browser light keys in every open browser).

- **N-015** · raised `2026-0731-0731-soundfont-source` · value low
  Expose meltysynth reverb/chorus enable as an `sfont` source param, in case
  doubling with the strip reverb bothers mixes (`NewSFSynth` leaves both on,
  `audio/source/soundfont.go`).

- **N-016** · raised `2026-0731-0753-sustain-drums` · value low
  Sostenuto (CC66) and soft pedal (CC67), following the `evPedal` pattern
  "if ever wanted". meltysynth doesn't implement CC66, so sostenuto would need
  our own voice tracking. Candidate for Non-goals.

- **N-017** · raised `2026-0731-0835-midi-playback` · value low
  MIDI-player seek bar, and the loop toggle applying mid-song instead of at the
  next play. Both need position→message-index access that
  `meltysynth.MidiFileSequencer` keeps private, i.e. vendoring a small
  sequencer. "Only worth it for scrubbing."

- **N-018** · raised `2026-0731-0842-drum-kit-selector` · value low
  Per-kit key maps / labels on the virtual piano when drums mode is on.

- **N-019** · raised `2026-0731-0842-drum-kit-selector` · value low
  MIDI program-change passthrough from Web MIDI input to the program/kit
  selects. `onMIDIMessage` (`web/assets/app.js`) handles only note-on/off and
  CC64 today.

- **N-020** · raised `2026-0731-1243-bank-drum-kit-names` · value low
  Melodic program select from the bank's own bank-0 presets instead of the
  fixed `gmPrograms` table (`web/ui/gm.go`); generalize `BankDrumKits` to
  `BankPresets(path, bank)`.

- **N-021** · raised `2026-0731-1301-metronome-count-in` · value low
  If sample-accurate click spacing ever matters, move the beat clock into the
  engine (samples-per-beat counter in the callback); the browser only
  starts/stops it. Contingent.

- **N-022** · raised `2026-0731-1316-tap-tempo-tutorial-progress` · value low
  Richer per-lesson stats readout while playing (last vs best), beyond the
  one-line `progressNote()` preview. Carried through six docs untouched.

- **N-023** · raised `2026-0731-1728-tutorial-countin-toggle` · value low
  Split the count-in toggle "if desired": Listen keeps its count-in while Start
  obeys the setting (one line at the `countIn()` gate).

- **N-024** · raised `2026-0731-1735-countin-metro-bpm` · value medium
  Tutorial count-in counts one bar (honor `#metro-beats`, as the record
  count-in already does) instead of a fixed four; update the tooltip's "four
  pacing clicks". Still `countIn(4, …)` at both call sites in `app.js`.

- **N-025** · raised `2026-0731-1747-bass-xpander-cross-fader-plugins` · value medium
  SSE-push param changes so linked `cross_fader` sliders (shared package-level
  `position`) update live across channel panels; today they show a stale
  position until reload.

- **N-026** · raised `2026-0731-1747-bass-xpander-cross-fader-plugins` · value low
  Dedicated crossfader UI control (big horizontal fader) instead of the
  auto-generated module slider.

- **N-027** · raised `2026-0808-1332-getting-started-tour` · value low
  Per-section tour state if the tour ever gains chapters worth re-running;
  `tour.seen` is one global flag, which the doc judged right for now.
  Contingent.

- **N-028** · raised `2026-0808-1332-getting-started-tour` · value low
  "Skip to section" jump in the tour balloon head (dots jump by position, not
  by name).

- **N-029** · raised `2026-0811-2014-soundcraft-ui24r-console-restyle` · value low
  Latching clip indicator on the meters (solid `#E83B45` cap). Premise partly
  off: the meter publishes a *decaying* peak (`mixer/meter.go`, ~0.92/block),
  not a latched over, so it needs a latch, either in the meter or client-side
  on peak ≥ 0 dBFS.

- **N-030** · raised `2026-0811-2014-soundcraft-ui24r-console-restyle` · value low
  Master meter + clock in the top bar, Ui24R TOPNAV style.

- **N-031** · raised `2026-0811-2014-soundcraft-ui24r-console-restyle` · value low
  Let plugin modules declare their own `--fam` family colour. Contingent on a
  plugin wanting one.

- **N-033** · raised `2026-0811-2014-soundcraft-ui24r-console-restyle` · value low
  SOLO key + solo bus. Left out of the restyle on purpose ("a control that does
  nothing is worse than a missing one"); needs `mixer.Channel` state,
  master-sum bypass logic and scene persistence.

- **N-039** · raised `2026-1118-0719-reliability-pass` · value medium
  Decide network exposure. `httpAddr` defaults to `:8000` (all
  interfaces), and source endpoints accept any filesystem path (`wav`,
  `sfont`, `midi`). The reliability pass added a same-origin check on POSTs
  (blocks drive-by web pages) and regular-file/size checks on WAVs, but any
  machine on the LAN can still drive the console and point it at any
  readable file. Options: default to `127.0.0.1` (breaks a tablet used as a
  control surface), and/or restrict source paths to the configured
  soundbanks/midifiles/recordings dirs. A product decision, not a fix.

- **N-040** · raised `2026-1118-0719-reliability-pass` · value medium
  rweb (upstream, `rohanthewiz/rweb` v0.1.26) has no `recover()` in
  `handleConnection` and sizes `make([]byte, contentLen)` from the request's
  `Content-Length` before any handler runs, so one crafted request can still
  crash the process. go-daw's `recoverPanics` middleware only covers panics
  inside handlers. Fix belongs in rweb: recover per connection, cap
  Content-Length.

- **N-041** · raised `2026-1118-0719-reliability-pass` · value low
  Recorder overruns are spliced out instead of zero-filled, so every
  dropped block shifts the rest of the take earlier. Now reported in the UI
  (count at stop, SSE `recDropped`); writing silence for the dropped span
  would keep the timeline intact.

- **N-042** · raised `2026-1118-0719-reliability-pass` · value low
  Mute (and other toggles) are set optimistically per tab and never
  broadcast, so a second tab shows stale state and needs two clicks. Push
  console state changes over SSE, or roll back on a failed POST.

- **N-043** · raised `2026-1118-0719-reliability-pass` · value low
  Module param/remove requests address inserts by chain index captured at
  page render; a stale tab can edit the wrong module after another tab
  edits the chain. Give module instances stable IDs.

- **N-044** · raised `2026-1118-0719-reliability-pass` · value low
  Accessibility gaps: faders and slider labels have no accessible name
  (`<label>` not bound to its input), log sliders expose 0..1000 instead of
  the real value (`aria-valuetext`), mute/REC state lives only in `data-on`
  (`aria-pressed`), and Space is captured for the pedal even on a focused
  button.

- **N-045** · raised `2026-1118-0719-reliability-pass` · value low
  Sample-rate and CPU assumptions in DSP: gate detector coefficients
  (`0.9`/`0.9993`, `dsp/gate.go`) are tuned for 48 kHz, and the recorder
  ring is sized in samples (only ~0.7 s at 192 kHz). Reverb combs settle
  into float32 denormals in silence, measured free on Apple M3 but slow on
  Intel Macs; flush tiny values in comb/allpass/biquad state.

- **N-046** · raised `2026-1118-0719-reliability-pass` · value low
  `PolySynth` spends four `math.Sin` per voice per sample (~69 µs per
  256-frame block at 16 voices, ~1.3% of the block). One `Sincos` plus
  Chebyshev recursion or a wavetable would cut it 3-4x. Only worth it if
  voice count grows.

- **N-047** · raised `2026-1118-0719-reliability-pass` · value low
  "Zero means unset" (`omitempty`) makes a source level of exactly 0 dBFS
  recall as the type's default (-18/-12/-6 dB). Server-side clamping caps
  levels at 0 dB, so a clamped +dB value lands on exactly this case. Use a
  pointer or explicit "set" flag in `SourceState`.

- **N-048** · raised `2026-1118-0719-reliability-pass` · value low
  `bankCache` (`audio/source/soundfont.go`) keeps every distinct bank path
  ever loaded, 30-200 MB each, for the life of the process. Evict banks no
  channel references.

## Validate

Items whose remaining work is purely testing: run, look, hear, measure, or
write or repair a test. No product change is planned unless the check finds a
defect.

- **N-005** · raised `2026-0729-1451-virtual-piano` · value low
  Play the virtual piano live in a browser: mouse/touch glissando, computer
  keys + Z/X octave, space sustain. The session ran only a scratchpad render
  check; later sessions drove notes over HTTP (`2026-0731-0731-soundfont-source`)
  but never the on-screen keys.

- **N-009** · raised `2026-0729-1545-web-midi` · value low
  Web MIDI with a physical keyboard: permission prompt, hot-plug, velocity-0
  note-off, CC64 pedal, and `midiPanic()` on unplug mid-chord. Never run on
  hardware.

- **N-014** · raised `2026-0729-1927-piano-tutorial` · value low
  Live tutorial play-through in a browser (Start and Listen). Since raised, the
  code under it gained count-in (with toggle and metronome pace) and progress
  persistence (✓ marks, stats line), none of which was run live either.

- **N-032** · raised `2026-0811-2014-soundcraft-ui24r-console-restyle` · value low
  Give the scratchpad screenshot harness (`shot.sh`) a home in
  `arch_test_scripts/`; the SSE-hang workaround (strip `app.js`, substitute an
  appearance stand-in) is the non-obvious part and will be rediscovered
  otherwise. The script itself was lost with the scratchpad, so it has to be
  rewritten from the doc's description.

- **N-049** · raised `2026-1118-0719-reliability-pass` · value medium
  Real-browser pass over the new client behavior. Scripted checks in Chrome
  covered toasts on a failed request, the keyboard piano with a fader
  focused, blur releasing a held key, and meter painting. Not yet seen
  live: the red fault rail and its toast (no fault was triggered), meter
  SSE closing and reopening as the tab hides and shows, a real Cmd-Tab and
  a hardware MIDI key during release-all, the metronome after the tab sits
  in the background, scene overwrite/delete confirms, and the take-saved
  toast after REC.

## Roadmap

Wanted but deferred. Seeded from the items session docs filed under
"Deferred / future ideas" (and the `.cats-todo` note calling the MIDI ones
"closer to a full session than a quick win").

- **N-001** · raised `2026-0729-1451-virtual-piano` · value low
  WebSocket note channel to replace the per-note HTTP POST (~1–2 ms on
  loopback); matters for remote play. rweb supports WS; nothing in-repo uses
  it yet.

- **N-006** · raised `2026-0729-1545-web-midi` · value low
  Native Go MIDI input (`gitlab.com/gomidi/midi/v2` + `rtmididrv`) for
  headless / lowest-latency play: `midiin` package, device-select API + rescan,
  persist the device.

- **N-007** · raised `2026-0729-1545-web-midi` · value low
  Pitch bend / mod wheel: new event kinds + per-voice pitch multiplier in
  `Read`.

- **N-008** · raised `2026-0729-1545-web-midi` · value low
  Multi-timbral routing (MIDI ch N → mixer ch N). The JS handler ignores the
  channel nibble; trivial once N-006 lands, also doable in JS.

- **N-010** · raised `2026-0729-1927-piano-tutorial` · value low
  Timing-scored lessons: score rhythm against each step's `Ms`.

- **N-011** · raised `2026-0729-1927-piano-tutorial` · value low
  Left-hand / two-hand lessons; the checker already supports arbitrary note
  sets per step.

- **N-012** · raised `2026-0729-1927-piano-tutorial` · value low
  Lesson unlock ordering. The rest of the original "per-user progress
  persistence" item shipped in `1603309` (`store/progress.go`).

- **N-013** · raised `2026-0729-1927-piano-tutorial` · value low
  MIDI-out lesson recording: capture a player's pass into a scene/WAV via the
  existing recorder.

## Non-goals

- **N-034** · declined `2026-0808-1332-getting-started-tour` — Tour steps that
  *demonstrate* by changing audio state (e.g. starting the click). A tour that
  changes state behind the reader's back is a worse default than one that
  invites them to press the button; `5218fc3` went the other way (asks the
  reader to act, then ticks).
- **N-035** · declined `2026-0811-2014-soundcraft-ui24r-console-restyle` — A
  canvas-painted console like the Ui24R's. DOM keeps server rendering, the
  tour's selector targeting, and keyboard/AT support.
- **N-036** · declined `2026-0811-2014-soundcraft-ui24r-console-restyle` —
  Embedding Soundcraft's fonts or art; they are theirs.

## Closed

Closures before this file existed live in the session docs' bodies.

- **N-037** · raised `2026-0731-1716-metronome-bpm-persist` · closed 2026-11-18, 2026-1118-0719-reliability-pass
  Persist metronome beats-per-bar (`metro.beats` setting, server-rendered
  select). Done in `c18857b`, a commit with no session doc; found during the
  seed rebuild.
- **N-038** · raised `2026-0731-1418-shift-click-record` · closed 2026-11-18, 2026-1118-0719-reliability-pass
  Pre-existing `unusedparams` lint at `controls.go:88` (noted in four docs).
  Fixed in `8c0b103`: `readoutText()` now uses its `param` argument.
