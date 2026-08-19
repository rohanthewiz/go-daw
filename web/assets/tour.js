// go-daw guided tour: a step engine that spotlights the real console, asks the
// user to actually perform each action, and detects completion by watching the
// same DOM signals the mixer already paints (data-on flags on mute/record/
// metronome, lit piano keys, module strips appearing, the scene list growing).
//
//   ┌──────────┐ goto(i)  ┌──────────────┐ install  ┌──────────────────┐
//   │   tour   ├─────────►│ card + ring  ├─────────►│ step.watch(done) │
//   │ (engine) │          │ over live UI │          │  DOM listeners   │
//   └────▲─────┘          └──────────────┘          └────────┬─────────┘
//        │  markDone() → ✓ beat → next()                     │
//        └───────────────────────────────────────────────────┘
//
// Design choices worth calling out:
//
//   - Separate file, zero coupling to app.js. app.js is a closed IIFE with no
//     event bus, and the tour deliberately doesn't open one: everything it
//     needs is already in the DOM, because the mixer renders its state there.
//     Reading those signals means every input route — mouse, computer
//     keyboard, hardware MIDI — satisfies a step identically, and the whole
//     feature (markup, styles, copy, logic) can be understood or deleted as a
//     unit.
//
//   - The tour survives page reloads. Structural changes in go-daw (source
//     swap, module add, scene save/recall) reload the page by design — the
//     server is the single source of truth — so a tour living only in memory
//     would die on its most interesting steps. Position and per-step
//     baselines persist in sessionStorage, and any step whose action reloads
//     is written to re-detect from post-reload DOM state:
//
//       step N installs → baseline recorded → user acts → POST → reload
//                                                                  │
//       resume at step N ◄── sessionStorage ◄─────────────────────-─┘
//       settled(pred) sees the DOM already differs from baseline → ✓ → N+1
//
//     sessionStorage rather than localStorage: a half-finished tour should
//     outlive a reload, not a closed tab.
//
//   - The tour never blocks the console. Only the two bookend steps dim the
//     page; every interactive step leaves the entire UI live, because the
//     point is to try it on real audio.
//
//   - CSS is injected from here instead of living in main.styl, and every
//     class is prefixed `tour-`. The piano lesson panel already owns the
//     `tut-` prefix (web/ui/tutorial.go) — two different tutorials share this
//     page, so their namespaces must not collide.
(function () {
  "use strict";

  var CARD_W = 340;        // px; the placement math below is written around it
  var ADVANCE_MS = 1200;   // "✓ nice" beat before the card moves on
  var BUSY_POLL_MS = 250;  // re-check cadence while the user is mid-edit
  var BUSY_MAX_MS = 12000; // give up waiting out an abandoned edit
  var POS_MS = 250;        // reposition cadence (sections open, page scrolls)
  var STATE_KEY = "godaw.tour";

  // ---- persisted position --------------------------------------------------

  // idx = the step to (re)enter; base = per-step "what did this look like
  // before the user touched it" snapshots, keyed by step key.
  var state = { idx: 0, base: {} };

  function loadState() {
    try {
      var s = JSON.parse(sessionStorage.getItem(STATE_KEY) || "null");
      if (!s || typeof s.idx !== "number") return null;
      if (!s.base) s.base = {};
      return s;
    } catch (_) { return null; }
  }

  function saveState() {
    try { sessionStorage.setItem(STATE_KEY, JSON.stringify(state)); } catch (_) {}
  }

  function clearSaved() {
    try { sessionStorage.removeItem(STATE_KEY); } catch (_) {}
  }

  // baseline records what the console looked like when a step first began, so
  // "did the user change this?" still has an answer after the reload the
  // change itself triggered. Written once per step key; re-entering the step
  // (or resuming into it) reuses the stored value rather than re-reading a
  // console the user has already altered.
  function baseline(key, read) {
    if (!(key in state.base)) {
      state.base[key] = read();
      saveState();
    }
    return state.base[key];
  }

  // ---- tiny DOM helpers ----------------------------------------------------

  function q(sel) { return document.querySelector(sel); }
  function countOf(sel) { return document.querySelectorAll(sel).length; }
  function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }

  function post(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
  }

  // ---- completion detectors ------------------------------------------------
  // Each factory returns watch(done) → cleanup(). done() may fire any number
  // of times; the engine's stepDone guard ignores repeats. Cleanup is
  // mandatory: a skipped or abandoned step must not leak listeners.

  // Fires when anything matching sel is clicked. Delegated at the document so
  // the detector doesn't care that the element was re-rendered by a reload.
  function clicked(sel) {
    return function (done) {
      var h = function (e) {
        if (e.target && e.target.closest && e.target.closest(sel)) done();
      };
      document.addEventListener("click", h, true);
      return function () { document.removeEventListener("click", h, true); };
    };
  }

  // Fires on any `type` event originating inside sel. Capture phase on
  // purpose: <details> dispatches "toggle" without bubbling, but every event
  // still traverses the ancestor chain on the way *down*, so one document-level
  // capturing listener sees them all.
  function fired(type, sel) {
    return function (done) {
      var h = function (e) {
        if (e.target && e.target.closest && e.target.closest(sel)) done();
      };
      document.addEventListener(type, h, true);
      return function () { document.removeEventListener(type, h, true); };
    };
  }

  // Fires when pred() first holds, watching the DOM for changes. attrs narrows
  // the observer to specific attribute names — worth doing, because the meter
  // stream rewrites `style` on ~13 elements twelve times a second and there's
  // no reason to wake this predicate for any of it.
  //
  // The predicate is evaluated synchronously inside the observer callback so
  // transient states are catchable: a piano key carries data-on="1" only while
  // the note is held.
  function domIs(pred, attrs) {
    return function (done) {
      var obs = new MutationObserver(function () { if (pred()) done(); });
      var opts = { subtree: true, childList: true, attributes: true };
      if (attrs) opts.attributeFilter = attrs;
      obs.observe(document.body, opts);
      return function () { obs.disconnect(); };
    };
  }

  // Fires if pred() already holds at step entry. This is the post-reload half
  // of every step whose action reloads the page: by the time the tour comes
  // back, the event that proved the action is long gone, but the console it
  // produced is right there in the DOM. Deferred one task so the card is
  // rendered before a ✓ can land on it.
  function settled(pred) {
    return function (done) {
      var t = setTimeout(function () { if (pred()) done(); }, 0);
      return function () { clearTimeout(t); };
    };
  }

  // Runs several detectors as one; the first to fire completes the step.
  function any(list) {
    return function (done) {
      var cleanups = list.map(function (w) { return w(done); });
      return function () {
        cleanups.forEach(function (c) { if (c) c(); });
      };
    };
  }

  // ---- the tour ------------------------------------------------------------
  // Copy style: one idea per step, "Try it" in bold wherever an action is
  // expected, and every interactive step is skippable — guided never means
  // trapped. Channel 1 is the worked example throughout so the user builds one
  // coherent strip instead of poking eight.

  var CH1 = '.strip[data-strip="1"]';

  function buildSteps() {
    return [
      {
        modal: true,
        title: "Welcome to go-daw 👋",
        body:
          "This is a live mixing console: <b>channels</b> with gate, filters, EQ, " +
          "compression, reverb and plugin inserts, routed through <b>group buses</b> " +
          "to a limited <b>master</b>, plus a playable piano and a master recorder." +
          "<br><br>This tour walks the whole signal path — about five minutes, " +
          "hands-on, on real audio. Steps that ask you to try something " +
          "<b>notice when you've done it</b> and tick themselves off; " +
          "<i>Skip</i> moves past any of them." +
          "<br><br>Some actions (changing a source, adding a plugin, saving a scene) " +
          "reload the page — that's the server re-rendering the truth. The tour " +
          "picks up exactly where it left off." +
          "<br><br>Leave any time with ✕ or <kbd>Esc</kbd>; reopen with the " +
          "<b>Tour</b> button or <kbd>?</kbd>.",
      },
      {
        key: "transport",
        target: ".transport",
        title: "The transport bar",
        body:
          "Everything global lives up here: <b>REC</b> (master-bus recording), the " +
          "<b>metronome</b>, and <b>scene memory</b> for the whole console." +
          "<br><br>The dot on the right is the meter stream — green means the " +
          "browser is connected to the engine's SSE feed and your meters are live.",
      },
      {
        key: "source",
        target: CH1 + " .src-row",
        title: "Give channel 1 a source",
        body:
          "Every channel starts silent. The <b>Src</b> selector picks what feeds " +
          "it: <kbd>osc</kbd> test tone, <kbd>synth</kbd> (playable), " +
          "<kbd>sfont</kbd> SoundFont, <kbd>midi</kbd> file player, <kbd>wav</kbd> " +
          "file, or <kbd>live</kbd> input from your interface." +
          "<br><br><b>Try it:</b> set channel 1's <b>Src</b> to <kbd>osc</kbd> — a " +
          "steady tone gives us something to mix for the next few steps. " +
          "(Already on <kbd>osc</kbd>? Flip it to <kbd>none</kbd> and back; the " +
          "step is watching for the change, not for a particular source.)" +
          "<br><br>Changing a source rebuilds it on the audio thread, so the page " +
          "reloads — the tour comes right back here.",
        watch: (function () {
          var sel = 'select[data-role="source-select"][data-id="1"]';
          var was = function () {
            var el = q(sel);
            return el ? el.value : "none";
          };
          var changed = function () {
            var el = q(sel);
            return !!el && el.value !== baseline("source", was);
          };
          // A <select>'s value change mutates nothing in the DOM, so the
          // live path is the change event and the post-reload path is
          // settled() against the persisted baseline.
          return any([fired("change", sel), settled(changed)]);
        })(),
      },
      {
        key: "fader",
        target: CH1 + " .fader-row",
        title: "Fader & meter",
        body:
          "The fader is channel gain in dB (−60 … +12); the readout under it is " +
          "the exact value, applied with a per-block ramp so moves never zipper." +
          "<br><br>Beside it the meter shows the post-fader signal: the filled bar " +
          "is RMS, the white tick is peak, and the scale runs −60 dB at the bottom " +
          "to 0 dBFS at the top — green through amber into red." +
          "<br><br><b>Try it:</b> ride channel 1's fader and watch the meter follow.",
        watch: fired("input", CH1 + " input.fader"),
      },
      {
        key: "mute",
        target: CH1 + " .mute-btn",
        title: "Mute",
        body:
          "<b>Try it:</b> click <b>MUTE</b> — the channel drops out instantly and " +
          "the button goes red. Click again to bring it back." +
          "<br><br>Mute is a parameter like any other, so it's captured in scenes " +
          "and applies without a click or pop.",
        watch: clicked(CH1 + " .mute-btn"),
      },
      {
        key: "eq",
        target: CH1 + ' [data-sect="eq"]',
        title: "Three-band EQ",
        body:
          "Low, Mid and High are full parametric bands (RBJ biquads): " +
          "<b>Frq</b> sweeps 20 Hz–20 kHz on a log scale, <b>Gain</b> cuts or " +
          "boosts ±18 dB, <b>Q</b> sets how wide the bell is." +
          "<br><br><b>Try it:</b> pull the Mid band's <b>Gain</b> around and listen " +
          "to the tone change. Coefficients are recomputed off the audio thread " +
          "and swapped in atomically, so sweeping is safe while sound is playing.",
        watch: fired("input", CH1 + ' [data-sect="eq"] input[type="range"]'),
      },
      {
        key: "dynamics",
        target: CH1 + ' [data-sect="comp"]',
        title: "Dynamics & filters",
        body:
          "Sections collapse to keep a strip scannable — click a heading to open " +
          "it. Above the EQ sit the <b>Gate</b> (with hysteresis, so it doesn't " +
          "chatter) and the <b>HPF/LPF</b>; below it the <b>Comp</b> and " +
          "<b>Reverb</b>." +
          "<br><br>The compressor has a soft knee, and a <b>Ratio</b> of 20 or " +
          "more turns it into a limiter." +
          "<br><br><b>Try it:</b> open <b>Comp</b>, switch it <b>On</b>, and pull " +
          "<b>Thresh</b> down until the meter starts holding back.",
        watch: any([
          fired("change", CH1 + ' [data-sect="comp"] input[type="checkbox"]'),
          fired("input", CH1 + ' [data-sect="comp"] input[type="range"]'),
        ]),
      },
      {
        key: "modules",
        target: CH1 + ' [data-sect="modules"]',
        title: "Plugin inserts",
        body:
          "<b>Modules</b> is the insert chain — builtins (tremolo, delay) plus any " +
          "external Go plugin found in <code>plugins/*.so</code>. Their controls " +
          "are generated from the module's own parameter metadata, so a new plugin " +
          "needs no UI code at all." +
          "<br><br><b>Try it:</b> pick something from <b>+ module…</b>. It's " +
          "inserted after the EQ and compressor, and <b>×</b> removes it again." +
          "<br><br>(Adding one reloads the page; the tour resumes here.)",
        watch: (function () {
          var sel = CH1 + " .mod";
          var grew = function () {
            return countOf(sel) > baseline("modules", function () { return countOf(sel); });
          };
          return any([settled(grew), domIs(grew, [])]);
        })(),
      },
      {
        key: "route",
        target: CH1 + " .route-row",
        title: "Bus routing",
        body:
          "<b>Bus</b> decides where the channel lands: straight to Master, or into " +
          "one of the group buses — drums to Grp 1, vocals to Grp 2, and so on, so " +
          "one fader rides the whole submix." +
          "<br><br><b>Try it:</b> send channel 1 to <b>Grp 1</b>. Routing is live: " +
          "no reload, no gap in the audio.",
        watch: (function () {
          var sel = CH1 + ' select[data-role="group-select"]';
          var was = function () {
            var el = q(sel);
            return el ? el.value : "0";
          };
          var changed = function () {
            var el = q(sel);
            return !!el && el.value !== baseline("route", was);
          };
          return any([fired("change", sel), settled(changed)]);
        })(),
      },
      {
        key: "buses",
        target: ".buses",
        title: "Groups & master",
        body:
          "Here are the group buses you just routed into, and the <b>master</b> at " +
          "the end of the chain." +
          "<br><br>Master carries an always-on safety limiter — it can't be " +
          "switched off, so a runaway gain stage clips politely instead of " +
          "slamming your speakers. Its meter is also what the recorder taps.",
      },
      {
        key: "metro",
        target: ".metro-box",
        title: "Metronome",
        body:
          "The browser keeps the clock and each beat asks the engine for a click, " +
          "mixed into your monitor path only — clicks never end up in a recording." +
          "<br><br>Set the tempo in the box, the meter beside it (2/4 … 6/8), or " +
          "just <b>tap it in</b>: three or more taps on <b>CLICK</b> inside two " +
          "seconds sets the BPM from your hand. Both settings persist." +
          "<br><br><b>Try it:</b> switch the metronome on.",
        watch: domIs(function () {
          var el = q("#metro-btn");
          return !!el && el.dataset.on === "1";
        }, ["data-on"]),
      },
      {
        key: "record",
        target: "#rec-btn",
        title: "Recording",
        body:
          "<b>REC</b> captures the master bus to <code>recordings/*.wav</code>. It " +
          "starts after a one-bar count-in at the metronome's tempo and meter, so " +
          "the pace is in your ear before the take begins — the readout counts it " +
          "down." +
          "<br><br>• press <b>REC</b> again during the count-in to abort" +
          "<br>• <b>shift-click</b> to skip the count-in and record immediately" +
          "<br>• while recording, the readout shows elapsed seconds" +
          "<br><br><b>Try it:</b> start a take — then press <b>REC</b> once more to " +
          "stop and finalize the WAV.",
        watch: clicked("#rec-btn"),
      },
      {
        key: "scenes",
        target: ".scene-box",
        title: "Scene memory",
        body:
          "A <b>scene</b> is a snapshot of the entire console — every fader, EQ " +
          "curve, plugin parameter, route and mute — stored in go-daw's embedded " +
          "database." +
          "<br><br><b>Try it:</b> type a name and press <b>Save</b>. <b>Recall</b> " +
          "applies a saved scene live, through the same atomic parameter cells the " +
          "UI writes, so the audio never stops." +
          "<br><br>(Saving reloads the page so the picker rebuilds; the tour " +
          "resumes here.)",
        watch: (function () {
          var sel = '#scene-list option[value]:not([value=""])';
          var grew = function () {
            return countOf(sel) > baseline("scenes", function () { return countOf(sel); });
          };
          return any([settled(grew), domIs(grew, [])]);
        })(),
      },
      {
        key: "piano",
        target: "#piano",
        title: "The virtual piano",
        body:
          "25 keys driving a polyphonic synth on whichever channel the <b>Ch</b> " +
          "selector points at. Picking a channel that isn't already playable " +
          "installs a synth on it (and reloads)." +
          "<br><br>• <b>click or drag</b> across the keys — how low you strike sets " +
          "velocity" +
          "<br>• computer keyboard: <kbd>A</kbd> <kbd>S</kbd> <kbd>D</kbd> <kbd>F</kbd>… " +
          "for whites, <kbd>W</kbd> <kbd>E</kbd> <kbd>T</kbd> <kbd>Y</kbd> for sharps" +
          "<br>• <kbd>Z</kbd> / <kbd>X</kbd> shift an octave, <kbd>Space</kbd> is the " +
          "sustain pedal" +
          "<br>• a hardware MIDI keyboard shows up in the picker automatically" +
          "<br><br><b>Try it:</b> play a note, any way you like.",
        watch: domIs(function () {
          return !!q('.piano-key[data-on="1"]');
        }, ["data-on"]),
      },
      {
        key: "lessons",
        target: "#tutorial",
        title: "Piano lessons",
        body:
          "The other tutorial in the app: real lessons — scales, first melodies, " +
          "chord progressions. Target keys get amber rings, wrong notes flash red " +
          "and are counted, and chord steps wait until you're holding the whole " +
          "shape. Passes are saved, so completed lessons keep a ✓ and a best score." +
          "<br><br><b>Try it:</b> pick a lesson and press <b>Listen</b> to hear it, " +
          "or <b>Start</b> to play it yourself. <b>Count-in</b> gives you four " +
          "clicks at the metronome tempo first.",
        watch: clicked("#tut-start, #tut-demo"),
      },
      {
        modal: true,
        title: "You're ready to mix 🎉",
        body:
          "That's the whole path: <b>source → strip → bus → master → record</b>, " +
          "with scenes to remember any of it." +
          "<br><br><table class='tour-keys'>" +
          "<tr><td><kbd>A</kbd>…<kbd>;</kbd></td><td>play white keys</td>" +
          "<td><kbd>Space</kbd></td><td>sustain</td></tr>" +
          "<tr><td><kbd>W</kbd> <kbd>E</kbd> <kbd>T</kbd></td><td>play sharps</td>" +
          "<td><kbd>Z</kbd> / <kbd>X</kbd></td><td>octave down / up</td></tr>" +
          "<tr><td><kbd>?</kbd></td><td>this tour</td>" +
          "<td><kbd>Esc</kbd></td><td>close the tour</td></tr>" +
          "</table>" +
          "<br>Next stops: drop an <code>.sf2</code> into <code>soundbanks/</code> " +
          "for real instruments, a <code>.mid</code> into <code>midifiles/</code> " +
          "to play songs, or write your own plugin — the README walks through one " +
          "end to end.",
      },
    ];
  }

  // ---- engine --------------------------------------------------------------

  var steps = [];
  var active = false;
  var stepDone = false;
  var cleanupWatch = null; // current step's detector teardown
  var advanceTimer = null; // pending auto-advance after a ✓
  var advanceBy = 0;       // wall-clock deadline for waiting out a busy user
  var reposTimer = null;
  var backdrop, ring, card;

  function start(resumeAt) {
    if (active) return;
    active = true;
    steps = buildSteps();
    // Opening the tour once is enough to stop it auto-launching forever after;
    // the flag is persisted server-side (bytdb) like the other UI preferences,
    // so it follows the console rather than one browser profile.
    var btn = q("#tour-btn");
    if (btn) {
      btn.dataset.on = "1";
      if (btn.dataset.seen === "0") {
        btn.dataset.seen = "1";
        post("/api/setting", { key: "tour.seen", value: "1" });
      }
    }
    buildDom();
    reposTimer = setInterval(position, POS_MS);
    goTo(typeof resumeAt === "number" ? resumeAt : 0);
  }

  function exit() {
    if (!active) return;
    active = false;
    teardownStep();
    clearInterval(reposTimer);
    backdrop.remove();
    ring.remove();
    card.remove();
    state = { idx: 0, base: {} };
    clearSaved();
    var btn = q("#tour-btn");
    if (btn) btn.dataset.on = "0";
  }

  function toggle() {
    if (active) exit();
    else start(0);
  }

  function goTo(i) {
    if (i < 0 || i >= steps.length) return;
    teardownStep();
    state.idx = i;
    stepDone = false;
    saveState(); // survive the reload this step's action may trigger

    var step = steps[i];
    renderCard(step);
    // Strips are taller than most windows and the channel row scrolls
    // sideways, so a target can easily be off-screen. Scroll on entry only —
    // doing it on every reposition tick would fight the user's own scrolling.
    var target = step.target ? q(step.target) : null;
    if (target && target.scrollIntoView) {
      target.scrollIntoView({ block: "center", inline: "nearest", behavior: "smooth" });
    }
    position();

    if (step.watch) cleanupWatch = step.watch(markDone);
  }

  function teardownStep() {
    clearTimeout(advanceTimer);
    advanceTimer = null;
    if (cleanupWatch) cleanupWatch();
    cleanupWatch = null;
  }

  function markDone() {
    if (!active || stepDone) return;
    stepDone = true;
    card.querySelector(".tour-check").classList.add("show");
    var next = card.querySelector(".tour-next");
    next.textContent = nextLabel();
    next.classList.add("tour-accent");
    advanceBy = Date.now() + BUSY_MAX_MS;
    scheduleAdvance(ADVANCE_MS);
  }

  // busy reports that the user is mid-edit — typing a scene name, or holding a
  // fader. Yanking the spotlight away then feels like the app stole the click.
  function busy() {
    var el = document.activeElement;
    if (!el) return false;
    var tag = el.tagName;
    return tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA";
  }

  // A beat of "✓ nice" before moving on: instant jumps read as the app
  // stealing the click, and too long and the user reaches for Next anyway. If
  // the beat lands while they're busy we re-arm at a tighter cadence and wait
  // them out — but only up to BUSY_MAX_MS, since an abandoned edit looks
  // exactly like an active one and a card that never moves is worse than one
  // that moves a moment early.
  function scheduleAdvance(delay) {
    clearTimeout(advanceTimer);
    advanceTimer = setTimeout(function () {
      if (busy() && Date.now() < advanceBy) {
        scheduleAdvance(BUSY_POLL_MS);
        return;
      }
      next();
    }, delay);
  }

  function next() {
    if (state.idx >= steps.length - 1) exit();
    else goTo(state.idx + 1);
  }

  function back() {
    if (state.idx > 0) goTo(state.idx - 1);
  }

  // ---- DOM -----------------------------------------------------------------

  function buildDom() {
    backdrop = document.createElement("div");
    backdrop.id = "tour-backdrop";
    ring = document.createElement("div");
    ring.id = "tour-ring";
    card = document.createElement("div");
    card.id = "tour-card";
    document.body.appendChild(backdrop);
    document.body.appendChild(ring);
    document.body.appendChild(card);
  }

  function nextLabel() {
    var step = steps[state.idx];
    if (state.idx === 0) return "Start the tour ›";
    if (state.idx === steps.length - 1) return "Finish ✓";
    // A step still waiting on its detector offers "Skip" — same button, honest
    // label.
    return step.watch && !stepDone ? "Skip ›" : "Next ›";
  }

  function renderCard(step) {
    var n = steps.length;
    var backLabel = state.idx === 0 ? "Not now" : "‹ Back";
    card.innerHTML =
      '<div class="tour-head">' +
        '<span class="tour-title">' + step.title + "</span>" +
        '<span class="tour-count">' + (state.idx + 1) + "/" + n + "</span>" +
        '<button class="tour-close" title="Close the tour (reopen with ?)">✕</button>' +
      "</div>" +
      '<div class="tour-body">' + step.body + "</div>" +
      '<div class="tour-check">✓ Nice — got it!</div>' +
      '<div class="tour-foot">' +
        '<button class="tour-back">' + backLabel + "</button>" +
        '<div class="tour-bar"><div class="tour-bar-fill" style="width:' +
          ((state.idx / (n - 1)) * 100) + '%"></div></div>' +
        '<button class="tour-next ' + (step.watch ? "" : "tour-accent") + '">' +
          nextLabel() + "</button>" +
      "</div>";

    card.querySelector(".tour-close").addEventListener("click", exit);
    card.querySelector(".tour-next").addEventListener("click", next);
    card.querySelector(".tour-back").addEventListener("click", function () {
      if (state.idx === 0) exit();
      else back();
    });
  }

  // Placement runs on entry and every POS_MS while open — sections open and
  // close, the page scrolls, the transport is sticky. Polling is both cheaper
  // and far more robust than observing every possible mover.
  //
  //   room below?  → card under the target (the usual case: transport, strips)
  //   room above?  → card over it (piano and tutorial panels at the page foot)
  //   neither      → pin to the bottom and slide the card clear sideways
  function position() {
    if (!active) return;
    var step = steps[state.idx];
    var W = window.innerWidth, H = window.innerHeight;

    if (step.modal) {
      backdrop.style.display = "block";
      ring.style.display = "none";
      card.classList.add("tour-center");
      card.style.left = card.style.top = "";
      return;
    }
    backdrop.style.display = "none";
    card.classList.remove("tour-center");

    var target = step.target ? q(step.target) : null;
    var rect = target ? target.getBoundingClientRect() : null;
    var cardH = card.offsetHeight || 220;
    var x, y;

    if (rect && rect.width > 0) {
      var pad = 5;
      ring.style.display = "block";
      ring.style.left = (rect.left - pad) + "px";
      ring.style.top = (rect.top - pad) + "px";
      ring.style.width = (rect.width + pad * 2) + "px";
      ring.style.height = (rect.height + pad * 2) + "px";

      x = clamp(rect.left, 10, W - CARD_W - 10);
      if (rect.bottom + cardH + 24 < H) {
        y = rect.bottom + 14;
      } else if (rect.top - cardH - 24 > 0) {
        y = rect.top - cardH - 14;
      } else {
        // Target taller than the free space (a whole channel strip): sit
        // beside it instead, preferring the roomier side.
        y = clamp(rect.top, 10, H - cardH - 16);
        x = rect.right + CARD_W + 24 < W
          ? rect.right + 14
          : clamp(rect.left - CARD_W - 14, 10, W - CARD_W - 10);
      }
    } else {
      // No target (or it isn't rendered): bottom-center keeps the console,
      // transport and piano all visible.
      ring.style.display = "none";
      x = (W - CARD_W) / 2;
      y = H - cardH - 24;
    }
    card.style.left = x + "px";
    card.style.top = y + "px";
  }

  // ---- styles --------------------------------------------------------------
  // Colors mirror main.styl's theme constants (panel/line/ink/accent) so the
  // tour looks native; they're restated here deliberately to keep this module
  // drop-in self-contained.
  function injectStyles() {
    if (q("#tour-style")) return;
    var style = document.createElement("style");
    style.id = "tour-style";
    style.textContent = [
      "#tour-backdrop{position:fixed;inset:0;display:none;background:rgba(10,12,16,0.6);z-index:40}",
      "#tour-ring{position:fixed;display:none;pointer-events:none;z-index:45;",
      "  border:2px solid #4da3ff;border-radius:8px;transition:all .25s ease;",
      "  animation:tour-pulse 1.8s ease-out infinite}",
      "@keyframes tour-pulse{",
      "  0%{box-shadow:0 0 0 0 rgba(77,163,255,.45)}",
      "  70%{box-shadow:0 0 0 12px rgba(77,163,255,0)}",
      "  100%{box-shadow:0 0 0 0 rgba(77,163,255,0)}}",
      "#tour-card{position:fixed;width:" + CARD_W + "px;z-index:50;",
      "  background:rgba(33,36,44,.98);border:1px solid #3a70a8;border-radius:10px;",
      "  box-shadow:0 8px 30px rgba(0,0,0,.55);padding:12px 14px;color:#cdd3e0;",
      "  font:12.5px/1.55 -apple-system,'Helvetica Neue',sans-serif;",
      "  transition:left .25s ease,top .25s ease}",
      "#tour-card.tour-center{width:430px;left:50%!important;top:50%!important;transform:translate(-50%,-50%)}",
      ".tour-head{display:flex;align-items:baseline;gap:8px;margin-bottom:6px}",
      ".tour-title{font-weight:700;font-size:14px;color:#4da3ff;flex:1}",
      ".tour-count{font-size:11px;color:#7d8496;font-variant-numeric:tabular-nums}",
      ".tour-close{background:none;border:none;color:#cdd3e0;opacity:.55;cursor:pointer;font-size:13px;padding:0 2px}",
      ".tour-close:hover{opacity:1}",
      ".tour-body b,.tour-body i{color:#eef3f8}",
      ".tour-body kbd,.tour-body code{font-family:ui-monospace,Menlo,monospace;font-size:11px;",
      "  background:#16181d;border:1px solid #383d4a;border-radius:4px;padding:1px 5px;white-space:nowrap}",
      ".tour-check{display:none;margin-top:8px;font-weight:600;color:#37c66a}",
      ".tour-check.show{display:block}",
      ".tour-foot{display:flex;align-items:center;gap:10px;margin-top:10px}",
      ".tour-foot button{background:#2a2e38;color:#cdd3e0;border:1px solid #383d4a;",
      "  border-radius:6px;padding:5px 12px;cursor:pointer;font:inherit;font-size:12px}",
      ".tour-foot button:hover{background:rgba(77,163,255,.18)}",
      ".tour-foot .tour-accent{background:rgba(77,163,255,.22);border-color:#4da3ff;font-weight:600}",
      ".tour-bar{flex:1;height:4px;border-radius:2px;background:#383d4a;overflow:hidden}",
      ".tour-bar-fill{height:100%;background:#4da3ff;border-radius:2px;transition:width .3s ease}",
      ".tour-keys{border-collapse:collapse;width:100%}",
      ".tour-keys td{padding:2px 8px 2px 0;font-size:12px;color:#7d8496}",
    ].join("\n");
    document.head.appendChild(style);
  }

  // ---- boot ----------------------------------------------------------------

  injectStyles();

  var tourBtn = q("#tour-btn");
  if (tourBtn) tourBtn.addEventListener("click", toggle);

  // "?" opens the tour, Esc closes it. Both honor the same "never hijack
  // typing" rule app.js uses for the piano keys, so neither steals a keystroke
  // meant for the scene name or the BPM box.
  window.addEventListener("keydown", function (e) {
    var tag = document.activeElement && document.activeElement.tagName;
    if (tag === "INPUT" || tag === "SELECT" || tag === "TEXTAREA") return;
    if (e.key === "?") toggle();
    else if (e.key === "Escape" && active) exit();
  });

  // Resume a tour that a reload interrupted; otherwise open it unprompted the
  // very first time this console is used (data-seen comes from the server).
  var saved = loadState();
  if (saved) {
    state = saved;
    start(saved.idx);
  } else if (tourBtn && tourBtn.dataset.seen === "0") {
    start(0);
  }
})();
