# Session: Living Next list + reliability / performance / usability pass

- **Session ID**: `f71cd842-040a-40b6-8c93-69f837efe88d`
- **Date**: 2026-11-18 07:19
- **Branch**: `main`
- **Previous session doc**: `2026-0811-2014-soundcraft-ui24r-console-restyle.md`
  (commit `5218fc3`, "Verify tour steps the reader actually performs", landed
  after it with no session doc of its own)

## What was asked

1. `/next-list seed`: build the living follow-up list from session-doc history.
2. Mid-way: "take a deep look at go-daw and find any opportunities to improve
   reliability, performance, and usability, and start executing the plan."

## Part 1: the living Next list (`ai_docs/todo/next-list.md`)

Seeded from all 19 earlier session docs (no `## Next` heading existed yet;
each doc had a differently named tail section: "Possible next steps",
"Deferred / future ideas", "Notes / future ideas", …). 38 items:

- **Open 21, Validate 4, Roadmap 8, Non-goals 3, Closed 2.**
- Roadmap was seeded only from items the docs filed under "Deferred / future
  ideas" (plus the `.cats-todo/todos.json` note calling WebSocket notes,
  native MIDI and pitch bend "closer to a full session than a quick win").
- Found done during the rebuild: **N-037** persist beats-per-bar (done in
  `c18857b`, a commit with no session doc) and **N-038** the recurring
  `unusedparams` lint at `controls.go:88` (fixed by the restyle).
- Premise corrected: **N-029** (clip indicator) — the meter publishes a
  *decaying* peak, not a latched over, so a latch is needed.
- **N-032** (screenshot harness): the scratchpad `shot.sh` is gone; it has to
  be rewritten from the restyle doc's description.

## Part 2: the review

Two reviewers ran in parallel (web layer; audio/DSP/store/record) while the
engine, mixer and recorder were read directly. Every finding acted on was
re-verified in the source first. Escape analysis showed no heap allocation in
any Process/Read path — the realtime *allocation* contract held; the problems
were input validation and state recovery.

### Engine-killers fixed (hang / crash / permanent silence)

| Problem | Fix |
|---|---|
| One NaN/Inf sample latched forever into biquad z1/z2, compressor envelope, reverb combs; via the master limiter the whole console went silent until restart, and NaN meter levels made `encoding/json` reject every SSE frame | `dsp.NonFinite` scan at the end of every `Channel.ProcessBlock`: silence the block, `Reset()` gate/filters/EQ/comp/reverb, count the fault. Same guard on the master before the limiter. New `Reset` methods are allocation-free (`clear()`). |
| Module params unvalidated: flanger `depth=1e20` → `for idx < 0 { idx += len }` never terminates **on the CoreAudio thread**; bass_xpander `drive=0` → NaN; delay `feedback=2` → Inf | `mixer.SetModuleParam` clamps to the module's own `ParamSpec` Min/Max and rejects unknown IDs; used by the HTTP handler and scene recall. Flanger wrap is now one bounded correction + range guard. |
| A plugin panic in `Process` unwinds into cgo and kills the process | `processInsert` wraps each insert in a deferred recover; the chain is now an `insertChain{mods, failed atomic.Uint64}` so a panicking module is bypassed for that chain generation (mask resets when the chain is replaced). Loader `recover`s per plugin and rejects nil/empty-name factories. |
| WAV: empty data chunk + Loop → `Read` spins forever on the audio thread; `bitsPerSample<8` divide-by-zero; rate 0 → `make(int(+Inf))`; WAVE_FORMAT_EXTENSIBLE int32 decoded as float; `/dev/zero` read forever | `LoadWav` stats first (regular file, ≤2 GiB), validates bits ∈ {16,24,32} and rate 1k–768k, resolves the EXTENSIBLE SubFormat, decodes non-finite float samples as 0, rejects zero-frame results; `Read` returns 0 on an empty source. |
| rweb has no `recover()` anywhere | `web/middleware.go` `recoverPanics` (first middleware) → logged 500. |
| Scene recall Set cells directly, skipping all clamps (`comp.ratio 0` → slope 1/0) | `applyStrip` routes every strip value through `Channel.ApplyParam`; bus gains clamped −60..+12; `ApplyParam` rejects non-finite values (clamps can't catch NaN). |
| Source levels/freq unclamped (`osc.level 1000` = 1e50 → Inf) | `mixer.ClampSourceLevel` (−60..0) / `ClampOscFreq` (20..5000), matching the sliders; used by the handler and `SetChannelSource`. |
| Config `sampleRate 0` → zero-length reverb buffers, NaN biquads | Validated at startup: 8000–192000 Hz, block 16–4096. |

### Data loss / correctness fixed

- **Recorder**: header checkpointed ~1 s (`wavWriter.checkpoint` = flush +
  pwrite sizes) so a crash leaves a playable file; files created `O_EXCL`
  with a `-2…` suffix on same-second collisions (previously the next take
  truncated the last); `StartRecording` uses `CompareAndSwap` (two starts
  orphaned a recorder); `frames` counts only blocks the ring accepted;
  `Dropped()` / `Err()` reported over SSE and in the stop response; refuses
  writes past the 4 GiB WAV limit; batch int16 conversion instead of a 2-byte
  `bufio.Write` per sample.
- **MidiPlayer**: auto-stop and position fold read the *live* loop flag
  while the sequencer latched it at Play, so toggling Loop mid-song stopped
  the song or left it "playing" over silence. Now an audio-thread-written
  `playLoop` mirror. Regression test verified to fail on the old code.
- **Module chain edits** CAS-retry (`editModules`), so concurrent adds no
  longer lose a module; **scene recalls** serialized by `Console.recallMu`;
  **`Biquad.Update`** serialized by a control-plane mutex.
- **HTML escaping**: the element builder writes attributes/text verbatim. A
  scene named `Bob's "Live" mix` broke its own `<option value>` (Recall and
  Delete then 404'd forever) and markup in a name was stored injection.
  `ui.esc` now wraps every user/file/plugin-derived string.
- **CSRF**: `sameOriginWrites` refuses POSTs with `Sec-Fetch-Site:
  cross-site` or an Origin whose host ≠ the `Host` header. Requests with no
  Origin (curl, smoke tests) pass.
- Scene names validated on save (trimmed, ≤64 runes, no control chars);
  recall returns `warnings` for sources/modules it could not restore.

### Usability fixed (`web/assets/app.js`, `main.styl`)

- **Toasts**: `post()` shows the server's error text for any non-2xx (read
  from a clone so callers can still read the body) and a "can't reach the
  server" message on network failure (resolves to a stand-in `{ok:false}`
  instead of an unhandled rejection). `reloadWith()` carries messages across
  structural reloads via sessionStorage. Repeats within 2 s collapse.
- REC stop reports the saved file, duration, dropped blocks, write errors.
- Scene save confirms an overwrite, delete confirms, recall shows warnings.
- **Keyboard piano died after touching any slider**: `isTypingTarget`
  matched every `<input>`, and a fader keeps focus after a drag. Now only
  text-entry inputs/select/textarea/contenteditable.
- **Stuck notes**: each note remembers the channel it started on (note-offs
  went to whatever channel was selected at release); `releaseAll()` on
  window blur, `pagehide`, hidden tab, and piano channel switch; pedal
  release follows its press channel.
- **Note ordering**: per channel+note promise chains, so a quick tap's
  note-off can't overtake its note-on across parallel connections (chords
  are not serialized).
- **Mute ramps** over one block (channel and group) instead of clicking.
- **Metronome** re-anchors after falling behind (background tab used to fire
  every missed beat back to back on return).
- **Fault badge**: SSE `faults` / `masterFaults` → red strip rail + tooltip +
  one toast; server logs a fault episode once (edge-triggered).
- **Meter stream** opens only while the tab is visible (six tabs used to
  exhaust the browser's per-host connections and freeze every POST).

### Performance

- Compressor/limiter below-knee fast path: skip per-sample `Log10` + `Pow`
  when the envelope is under `threshold − knee/2` (compared linearly against
  a per-block constant). Master limiter on a typical −12 dBFS signal:
  ~2.25 µs → ~0.9–1.1 µs per 256-frame block; output pinned identical to a
  slow reference by `TestCompressorFastPathMatchesReference`.
- Meter painting: cached elements, writes quantized to 0.5 % and skipped
  when unchanged (was ~40 document-wide `querySelector`s per frame at
  12.5 Hz). `meterLoop` skips encoding when no SSE client is connected.
- SSE hub `MaxDropped: 25` — passing an options struct had left it at 0,
  which *disables* eviction of stalled clients.

## Gotchas worth remembering

- **rweb `req.Host()` is parsed from the request-line URL**, which for
  ordinary origin-form requests is just the path, so it is always
  `"localhost"`. Use `req.Header("Host")`. The first draft of the origin
  check compared against `Host()` and would have rejected every POST.
- **Plugins pin package contents, not just go.mod**: any change to `dsp` or
  `module` requires rebuilding every `.so`
  (`go build -buildmode=plugin -o plugins/<name>.so ./plugins/src/<name>`).
  All three were rebuilt this session; README now says so.
- `atomic.Uint64.Or` (Go 1.23+) sets a bit without a CAS loop.
- A Go `recover()` around each insert costs one open-coded defer per module
  per block; only the panic path allocates.
- `gofmt -l` flags several files that were already unformatted at HEAD
  (`dsp/dynamics.go`, `gate.go`, `reverb.go`, `mixer/state.go`,
  `web/ui/page.go`, `transport.go`); mostly ASCII-diagram comments gofmt
  would reflow. Left alone deliberately.

## Verification

- `go build ./...`, `go vet ./...`, `go test -race ./...` all green;
  `node --check web/assets/app.js`; `go run ./arch_test_scripts/styl_check`.
- New tests: `mixer/mixer_test.go` (first tests in `mixer`: fault
  containment + recovery, non-finite ApplyParam, mute ramps, scene clamping,
  round trip, panicking insert bypass, concurrent AddModule, module param
  clamps), `record/record_test.go` (first in `record`: ring wraparound and
  overrun, checkpoint survives a simulated crash, conversion incl.
  NaN/Inf, 4 GiB refusal, end-to-end drain, no-overwrite),
  `audio/source/wavfile_test.go` (malformed headers, `/dev/zero`,
  extensible int32/float, non-finite float, empty Read), DSP fast-path
  equivalence, Reset recovery, MIDI loop toggle, scene-name escaping.
- Plugins rebuilt; `go run ./arch_test_scripts/plugin_smoke` OK.
- Live run on `127.0.0.1:8123` with a scratch config/DB (user's `godaw.db`
  untouched): origin check (same-origin 200, cross-origin 403, no-Origin
  200), `osc.level 1000` clamped, flanger `depth 1e20` clamped to 1 with
  meters still flowing, bad WAV / `/dev/zero` → 400 with server alive, two
  simultaneous REC starts → one 200 + one 409, header checkpoint visible
  mid-take, stop JSON with path/seconds, recall warning for a moved stem,
  escaped scene option in the page, no panics in the log.
- Chrome (scripted via the extension): meters painting, error toast on a
  failed WAV load, piano plays with a fader focused and not while typing in
  a text field, window blur releases a held key, no console errors on load.

## Next

Seeded: N-001–N-038 (living list created this session).
Closed: N-037, N-038 (found done during the seed). Declined: None.
Raised: N-039, N-040, N-041, N-042, N-043, N-044, N-045, N-046, N-047,
N-048, N-049. Deferred: None (Roadmap seeded with N-001, N-006, N-007,
N-008, N-010, N-011, N-012, N-013 from the docs' "Deferred" sections).
Promoted: None. Moved: None. Updated: None.
Full list: `ai_docs/todo/next-list.md`.
