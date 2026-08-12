# Session: Soundcraft Ui24R console restyle

- **Session ID**: `ac619ba2-3292-4fd5-b2ab-fe068cfa0813`
- **Date**: 2026-08-11 20:14
- **Branch**: `main`
- **Commit**: `8c0b103` — Restyle the console UI after the Soundcraft Ui24R

## What was asked

> "Please update the UI controls to better resemble the Soundcraft UI24R.
> Demo: https://www.soundcraft.com/ui24-software-demo/mixer.html"

Read as a presentation-layer task: restyle the surface, don't touch audio or
the control API.

## Getting the reference right (worth repeating)

`WebFetch` on the demo returned nothing useful — the Ui24R mixer is a single
5.6 MB HTML file that paints the whole console into a `<canvas>`, so there is
no meaningful DOM or CSS to read. The `<style>` block is 5 KB of dialogs and
`@font-face` rules and nothing else.

The real reference was **inside the JavaScript**. `curl` the page, then grep for
the palette object:

```
color = {bg:"#16181C", eqBg:"#18191D", vuBg:"#5E6168", ledText:"#00ffff",
         masterLabel:"#ECCA12", mLedText:"#E7E840", yellow:"#BCA633",
         red:"#A83B3B", green:"#47AF46", blue:"#3576B1", orange:"#b27635",
         tvMute:"#b53636", tvSolo:"#c3a72d", sub:"#ba5d81",
         EQ:"#A83B3B", DYN:"#47AF46", FX:"#3576B1", AUX:"#BCA633",
         INPUT:"#c4673a", gminbg:"#23252b", gminborder:"#0f1113", ...}
font = {stripChannel:"11pt open_sans_condensedbold", led:"18px umixled", ...}
```

Two more things fell out of the same file:

- The **VU meter** recipe: light-grey `#5E6168` track in a black surround, fill
  is `green → yellow → red` (stops at 0/.5/1) **quantised to 12px LED tiles**
  (`a = a - a%12`) with a repeat-y texture overlay. Clip is solid `#E83B45`.
- The **art**: `img1x/C-TOP.png`, `MUTE-SC-ON/OFF.png`, `SOLO-SC-ON.png`,
  `FADER-SILVER2.png`, `PAN-SLIDER.png`. Downloading and *looking* at these is
  what settled the button chrome (brightly edged dark key, red when muted) and
  the fader cap (milled silver with a dark index line).

Everything in the new stylesheet traces back to one of those. The palette is
the console's own, not an impression of it.

## The screenshot loop (the thing that made this possible)

Headless Chrome **never finishes loading** a page holding an SSE stream open —
`--virtual-time-budget` doesn't save you, the first attempt hung until the tool
timed out. Workaround, now in the scratchpad as `shot.sh`:

1. `curl` the server-rendered HTML and the compiled CSS to disk
2. strip the `<script src="/assets/app.js">` tag (the `EventSource` lives there)
3. substitute a stand-in script that reproduces what `app.js` does to the
   *appearance* of a fresh page — reveal the `.src-*` panel matching each
   channel's selected source, push levels into the meters, and optionally open
   every `<details>` (`OPEN=1`) or fake an open tour balloon (`TOUR=1`)
4. screenshot the local file

Combined with `curl -X POST /api/...` to set mutes, pans and EQ gains before
the shot, this gave a real visual iteration loop — and it earned its keep
immediately (see the `:empty` bug below).

## What changed

### `web/styles/main.styl` — full rewrite (~360 → ~940 lines)

Two mechanisms carry most of the design; both are documented in the file header
because everything else reads as noise without them:

- **`--fam`** — each processing section declares one family colour (EQ red,
  dynamics green, FX blue, input orange, pan cyan) and every slider fill,
  section tab, module border and indicator lamp inside it inherits it. Adding a
  section is one variable, not a restyle.
- **`--f`** — a unitless `0..1` slider position, rendered inline by the server
  and rewritten by `app.js` on input. Native ranges expose no hook for "the
  travel behind the cap", so the track gradient stops at `--f`. Unitless
  matters: the stylesheet does
  `calc(var(--f) * (100% - 11px) + 5.5px)` to inset the fill by half a cap so
  its edge sits *under* the cap instead of running past it at the ends. CSS
  allows number × length in `calc()`; percentage × length it does not.

Other decisions:

- **Slot drawn on the input's own `background`**, not on
  `::-webkit-slider-runnable-track`. Backgrounds lay out identically in every
  engine; the track pseudo-element's geometry (especially under
  `writing-mode: vertical-lr`) does not. The same declaration then serves both
  the horizontal params and the vertical faders, and only the *thumb* pseudo —
  which is reliable — needs per-engine rules (`-webkit-` and `-moz-`).
- **Centre-detented controls**: any range symmetric about zero (pan −1..1, EQ
  gain −18..18) gets a centre tick and a fill that grows outward from the
  middle, via `min()`/`max()` against the same `--f` expression. go-styl
  evaluates a bare `min(...)` as Stylus maths and errors — wrapping it in
  `calc(min(...))` passes it through as CSS.
- **Meter segmentation is an overlay on `.meter::after`, not a mask on the
  fill.** A mask on `.meter-rms` is anchored to that element's own box, so the
  LED grid would slide around as the fill height animates. An overlay keeps the
  segments fixed in the track — and it's what the original does (`vuTexture` is
  a repeat-y tile drawn over the bar).
- **Checkboxes became lamp keys** (`appearance: none` + a lit dot, with
  `:has(input:checked)` brightening the surrounding key) — the desk has no
  checkboxes, it has small keys with a lamp in them. Still real checkboxes for
  keyboard and assistive tech, and `:focus-visible` rings were added across
  sliders/buttons/selects since the slots are dark and a lost focus position is
  unrecoverable.
- **`align-items: stretch` on `.console`** + `margin-top: auto` on `.fader-row`
  is what makes it read as a console rather than a row of cards: every strip
  runs the full height of the tallest, so all the decks line up. The empty span
  that leaves on a bus strip is explained by a full-bleed rule above the deck.

### `web/ui/fader.go` — new, shared fader deck

`faderDeck(b, target, id, gainDB, meterKey)` renders the LED gain window, the
fader, the dB scale column and the meter for channels, groups and master alike
(the three had drifted into three copies of the same markup). `faderMinDB` /
`faderMaxDB` / `faderMarks` live together so the scale can't disagree with the
travel. `meterHTML` moved here from `channelstrip.go`.

### `web/ui/controls.go`

- `fillStyle()` emits `--f`.
- `readoutText()` finally uses its `param` argument (it had been accepted and
  ignored since the first build): console readouts are unit-aware —
  `L45` / `C` / `R100`, `1.2k`, `4.0:1`, `+3.0`, `35%`, ms without a decimal
  once it's long. Bucketed by the parameter's last dotted segment in
  `paramKind()`.
- `slider()` tags symmetric ranges `ctl-center` and pan `ctl-pan`.

### `web/assets/app.js` (+52 lines)

`fmtParam()` mirrors `readoutText`/`paramKind` case for case, and `setFill()`
keeps `--f` current. Both are called from the existing `updateReadout()`, so
no new listeners.

### `web/ui/controls_test.go` — new

Pins the readout table. This is the one genuinely fragile thing in the change:
the server prints the first value and the client reprints it the instant a
slider is grabbed, so any drift shows up as a number that *jumps on touch*.
Also pins `fillStyle` (clamping, degenerate range, log sliders arriving
pre-mapped).

Cross-checked the JS half by extracting `fmtParam` into a scratch file and
running the Go test's own table through `node` — all 20 cases agree.

### `arch_test_scripts/styl_check` — new

Compiles `web/styles/main.styl` and reports the error (or prints the CSS with
`-print`) without booting the audio engine, which needs a device and mic
permission. Used constantly this session; matches the existing
`arch_test_scripts/` convention and is now in the README.

## Bug found by looking at a screenshot

`.rec-time:empty { display: none }` was written into a shared LED-window rule
covering `.fader-readout, .rec-time, .midi-pos, #metro-bpm, .piano-oct,
.tut-progress`. **`:empty` matches any element with no children — including
every `<input>`** — so it silently hid the metronome BPM field. Caught by
diffing two screenshots; fixed by scoping the rule to the two spans that can
actually be blank.

## Verification

- `go build ./...`, `go vet ./...`, `go test ./...` — all clean
- `node --check web/assets/app.js`
- stylesheet compiled on every edit via `styl_check`
- ran the app on `:8123` against a scratch DB and screenshotted: default view,
  all-sections-open, mute engaged, module added, pan/EQ off centre, fader below
  unity, and the tour overlay

Load-bearing selectors were deliberately preserved — the tour targets
`.fader-row`, `.mute-btn`, `.route-row`, `.strip-group`, `.strip-master`,
`input[data-param="pan"]` and `details[data-sect=...]`; `app.js` finds readouts
by walking up to `.ctl` / `.fader-wrap`. `go test ./tour/` still passes.

## Deliberately not done

- **No SOLO key.** The Ui24R strip has one and it's tempting, but the mixer has
  no solo bus — a control that does nothing is worse than a missing one. It
  would need `mixer.Channel` state, master-sum bypass logic and scene
  persistence; that's a feature, not a restyle.
- **No canvas.** The original is fully canvas-painted. Staying in DOM keeps the
  server-renders-everything architecture, the tour's CSS-selector targeting,
  and keyboard/AT support intact.
- **No embedded Soundcraft fonts or art.** `open_sans_condensedbold` and
  `umixled` are theirs; the stylesheet uses a condensed-bold system stack
  (Avenir Next Condensed → Roboto Condensed → Arial Narrow) and a mono stack
  for the LED windows, and draws select carets from gradients rather than
  shipping an image.

## Possible next steps

- Clip indicator on the meters — the engine already tracks peak; a latching red
  cap segment (the original's solid `#E83B45`) is a small addition.
- The top bar could carry a master meter + clock the way the Ui24R's TOPNAV
  does, which would make the transport read less like a toolbar.
- `--fam` is currently set per section by `data-sect`. If plugin modules ever
  want to declare their own family colour, that's the hook.
- The screenshot harness (`shot.sh`) is scratchpad-only. If UI work continues,
  it's worth a home in `arch_test_scripts/` — the SSE-hang workaround is the
  non-obvious part and it'll be rediscovered otherwise.
