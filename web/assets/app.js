// go-daw mixer client. Deliberately small: the server renders all structure;
// this script only (1) sends parameter changes, (2) paints meters from the
// SSE stream, and (3) drives transport/scene/module actions. Structural
// changes reload the page so the server stays the single source of truth.
(function () {
  "use strict";

  // ---- helpers -------------------------------------------------------------

  function post(url, body) {
    return fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    }).then(function (r) {
      if (!r.ok) r.text().then(function (t) { console.error("POST", url, r.status, t); });
      return r;
    });
  }

  // Log-scaled sliders run 0..1000 and carry data-min/data-max; this mirrors
  // the Go-side mapping exactly so initial positions and live values agree.
  function sliderValue(el) {
    var v = parseFloat(el.value);
    if (el.dataset.log === "1") {
      var min = parseFloat(el.dataset.min), max = parseFloat(el.dataset.max);
      return min * Math.pow(max / min, v / 1000);
    }
    return v;
  }

  function fmt(v) {
    return Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1);
  }

  // Debounce per control key: sliders emit dozens of input events per second;
  // 30ms trailing-edge coalescing keeps the param feel instant while capping
  // request rate.
  var pending = {};
  function debounced(key, fn) {
    if (pending[key]) clearTimeout(pending[key]);
    pending[key] = setTimeout(function () { delete pending[key]; fn(); }, 30);
  }

  function updateReadout(el, value) {
    var wrap = el.closest(".ctl") || el.closest(".fader-wrap");
    if (!wrap) return;
    var ro = wrap.querySelector(".readout");
    if (ro) ro.textContent = fmt(value);
  }

  // ---- parameter dispatch --------------------------------------------------

  function sendParam(el) {
    var t = el.dataset.target, id = el.dataset.id, param = el.dataset.param;
    var value = el.type === "checkbox" ? (el.checked ? 1 : 0) : sliderValue(el);
    if (el.type === "range") updateReadout(el, value);

    var key = t + "/" + id + "/" + (el.dataset.idx || "") + "/" + param;
    debounced(key, function () {
      if (t === "ch") {
        post("/api/channel/" + id + "/param", { name: param, value: value });
      } else if (t === "grp") {
        post("/api/group/" + id + "/param", { name: param, value: value });
      } else if (t === "master") {
        post("/api/master/param", { name: param, value: value });
      } else if (t === "src") {
        post("/api/channel/" + id + "/source-param", { name: param, value: value });
      } else if (t === "mod") {
        post("/api/channel/" + id + "/module/param",
          { index: parseInt(el.dataset.idx, 10), id: param, value: value });
      }
    });
  }

  document.addEventListener("input", function (e) {
    var el = e.target;
    if (el.dataset && el.dataset.param && el.type === "range") sendParam(el);
  });
  document.addEventListener("change", function (e) {
    var el = e.target;
    if (el.dataset && el.dataset.param && el.type === "checkbox") sendParam(el);
  });

  // ---- clicks: mute, record, scenes, modules -------------------------------

  document.addEventListener("click", function (e) {
    var el = e.target;

    if (el.classList.contains("mute-btn")) {
      var on = el.dataset.on === "1" ? 0 : 1;
      el.dataset.on = String(on);
      var t = el.dataset.target, id = el.dataset.id;
      var url = t === "ch" ? "/api/channel/" + id + "/param" : "/api/group/" + id + "/param";
      post(url, { name: "mute", value: on });
      return;
    }

    if (el.id === "rec-btn") {
      if (el.dataset.on === "1") {
        post("/api/record/stop").then(function () { el.dataset.on = "0"; });
      } else {
        post("/api/record/start").then(function () { el.dataset.on = "1"; });
      }
      return;
    }

    if (el.id === "scene-save") {
      var name = document.getElementById("scene-name").value.trim();
      if (!name) return alert("Enter a scene name first");
      post("/api/scene/save", { name: name }).then(function (r) {
        if (r.ok) location.reload();
      });
      return;
    }
    if (el.id === "scene-recall") {
      var sel = document.getElementById("scene-list").value;
      if (!sel) return;
      post("/api/scene/recall", { name: sel }).then(function (r) {
        if (r.ok) location.reload();
      });
      return;
    }
    if (el.id === "scene-delete") {
      var del = document.getElementById("scene-list").value;
      if (!del) return;
      post("/api/scene/delete", { name: del }).then(function (r) {
        if (r.ok) location.reload();
      });
      return;
    }

    if (el.dataset && el.dataset.role === "mod-remove") {
      post("/api/channel/" + el.dataset.id + "/module/remove",
        { index: parseInt(el.dataset.idx, 10) }).then(function (r) {
        if (r.ok) location.reload();
      });
      return;
    }

    // MIDI transport buttons map directly onto the player's play/pause/stop
    // events; state feedback comes back through the SSE meter stream rather
    // than optimistic toggling, so the buttons always show the real state.
    if (el.dataset && (el.dataset.role === "midi-play" || el.dataset.role === "midi-pause" || el.dataset.role === "midi-stop")) {
      post("/api/channel/" + el.dataset.id + "/midi",
        { action: el.dataset.role.replace("midi-", "") });
      return;
    }

    if (el.dataset && el.dataset.role === "wav-load") {
      var id2 = el.dataset.id;
      var path = document.querySelector('input[data-role="wav-path"][data-id="' + id2 + '"]').value.trim();
      if (!path) return;
      post("/api/channel/" + id2 + "/source", { type: "wav", path: path }).then(function (r) {
        if (r.ok) location.reload();
      });
    }
  });

  document.addEventListener("change", function (e) {
    var el = e.target;
    if (!el.dataset) return;

    if (el.dataset.role === "source-select") {
      var id = el.dataset.id, type = el.value;
      showSourceRows(id, type);
      if (type === "wav") return; // wait for the Load button with a path
      var body = { type: type };
      if (type === "sfont") {
        // The bank select always has a value (the option is disabled when no
        // banks exist), so the source can install immediately on selection.
        var bankSel = document.querySelector('select[data-role="sfont-bank"][data-id="' + id + '"]');
        var progSel = document.querySelector('select[data-role="sfont-program"][data-id="' + id + '"]');
        if (!bankSel || !bankSel.value) return;
        body.path = bankSel.value;
        body.program = progSel ? parseInt(progSel.value, 10) : 0;
      }
      if (type === "midi") {
        // Same immediate-install contract: the midi option is disabled unless
        // both a bank and at least one song exist, so the selects have values.
        var mBankSel = document.querySelector('select[data-role="midi-bank"][data-id="' + id + '"]');
        var mFileSel = document.querySelector('select[data-role="midi-file"][data-id="' + id + '"]');
        if (!mBankSel || !mBankSel.value || !mFileSel || !mFileSel.value) return;
        body.bank = mBankSel.value;
        body.path = mFileSel.value;
      }
      post("/api/channel/" + id + "/source", body).then(function (r) {
        if (r.ok) location.reload();
      });
    }

    // Bank switch = structural change (new sample set, new synthesizer), so it
    // rebuilds the source and reloads. Program switch is live: it rides the
    // synth's event ring and never interrupts sounding notes.
    if (el.dataset.role === "sfont-bank") {
      var sfId = el.dataset.id;
      var sfProg = document.querySelector('select[data-role="sfont-program"][data-id="' + sfId + '"]');
      post("/api/channel/" + sfId + "/source", {
        type: "sfont",
        path: el.value,
        program: sfProg ? parseInt(sfProg.value, 10) : 0,
      }).then(function (r) { if (r.ok) location.reload(); });
    }
    if (el.dataset.role === "sfont-program") {
      post("/api/channel/" + el.dataset.id + "/source-param",
        { name: "sfont.program", value: parseInt(el.value, 10) });
    }

    // Bank or song switch = structural change (new synthesizer / new parsed
    // file), so both rebuild the source and reload — the sfont-bank pattern.
    if (el.dataset.role === "midi-bank" || el.dataset.role === "midi-file") {
      var mId = el.dataset.id;
      var mBank = document.querySelector('select[data-role="midi-bank"][data-id="' + mId + '"]');
      var mFile = document.querySelector('select[data-role="midi-file"][data-id="' + mId + '"]');
      if (!mBank || !mBank.value || !mFile || !mFile.value) return;
      post("/api/channel/" + mId + "/source", {
        type: "midi",
        bank: mBank.value,
        path: mFile.value,
      }).then(function (r) { if (r.ok) location.reload(); });
    }

    if (el.dataset.role === "group-select") {
      post("/api/channel/" + el.dataset.id + "/group",
        { group: parseInt(el.value, 10) });
    }

    if (el.dataset.role === "mod-select" && el.value) {
      post("/api/channel/" + el.dataset.id + "/module/add", { name: el.value })
        .then(function (r) { if (r.ok) location.reload(); });
    }
  });

  function showSourceRows(id, type) {
    var osc = document.querySelector('.src-osc[data-id="' + id + '"]');
    var syn = document.querySelector('.src-synth[data-id="' + id + '"]');
    var sf = document.querySelector('.src-sfont[data-id="' + id + '"]');
    var mid = document.querySelector('.src-midi[data-id="' + id + '"]');
    var wav = document.querySelector('.src-wav[data-id="' + id + '"]');
    if (osc) osc.dataset.visible = type === "osc" ? "1" : "0";
    if (syn) syn.dataset.visible = type === "synth" ? "1" : "0";
    if (sf) sf.dataset.visible = type === "sfont" ? "1" : "0";
    if (mid) mid.dataset.visible = type === "midi" ? "1" : "0";
    if (wav) wav.dataset.visible = type === "wav" ? "1" : "0";
  }

  // Initialize source sub-row visibility from the current selects.
  document.querySelectorAll('select[data-role="source-select"]').forEach(function (sel) {
    showSourceRows(sel.dataset.id, sel.value);
  });

  // ---- virtual piano -------------------------------------------------------
  // Note events bypass debounced() on purpose: unlike a fader stream, every
  // key event matters and order must hold (a swallowed note-off is a stuck
  // note). The server side is wait-free, so per-keypress POSTs are cheap.

  var pianoKeys = document.getElementById("piano-keys");
  if (pianoKeys) {
    var pianoSel = document.getElementById("piano-channel");
    var octLabel = document.getElementById("piano-oct");
    var base = 60; // MIDI note of the leftmost key; 60 = middle C (C4)
    var BASE_MIN = 24, BASE_MAX = 84; // C1..C6 (top key = base+24 = C8 max)

    var pianoCh = function () { return pianoSel ? pianoSel.value : "1"; };

    function light(note, on) {
      var idx = note - base;
      if (idx < 0 || idx > 24) return;
      var el = pianoKeys.querySelector('.piano-key[data-idx="' + idx + '"]');
      if (el) el.dataset.on = on ? "1" : "0";
    }

    // Every input route (mouse, computer keyboard, MIDI, tutorial demo)
    // funnels through these two calls, so the tutorial checker taps them here
    // rather than each producer separately.
    function noteOn(note, vel) {
      light(note, true);
      post("/api/channel/" + pianoCh() + "/note", { note: note, on: true, velocity: vel });
      tutNoteOn(note);
    }
    function noteOff(note) {
      light(note, false);
      post("/api/channel/" + pianoCh() + "/note", { note: note, on: false });
      tutNoteOff(note);
    }

    // Sustain pedal (CC64). Client-side state gate: the space bar autorepeats
    // and MIDI pedals stream continuous values, but the server only needs the
    // down/up transitions.
    var pedalDown = false;
    function pedalSet(down) {
      if (pedalDown === down) return;
      pedalDown = down;
      post("/api/channel/" + pianoCh() + "/pedal", { down: down });
    }

    function setBase(nb) {
      base = Math.max(BASE_MIN, Math.min(BASE_MAX, nb));
      if (octLabel) octLabel.textContent = "C" + (base / 12 - 1);
      // Clear highlights: lit keys now show different pitches. Held notes
      // still release correctly because note-offs use the stored pitch.
      pianoKeys.querySelectorAll('.piano-key[data-on="1"]').forEach(function (k) {
        k.dataset.on = "0";
      });
      // Re-aim tutorial guides: after a shift, the highlighted keys would
      // point at different pitches than the lesson expects.
      if (tutActive) guideStep();
    }

    // -- mouse / touch: press, release, and glissando (drag across keys) --

    var pointerNotes = {}; // pointerId -> sounding MIDI note

    function velFromEvent(e, el) {
      // Strike position as velocity: clicking low on the key plays louder,
      // mirroring how far a real key travels.
      var r = el.getBoundingClientRect();
      var y = (e.clientY - r.top) / r.height;
      return Math.min(1, Math.max(0.2, 0.35 + 0.65 * y));
    }

    pianoKeys.addEventListener("pointerdown", function (e) {
      var el = e.target.closest(".piano-key");
      if (!el) return;
      e.preventDefault();
      var note = base + parseInt(el.dataset.idx, 10);
      pointerNotes[e.pointerId] = note;
      noteOn(note, velFromEvent(e, el));
    });

    pianoKeys.addEventListener("pointerover", function (e) {
      if (pointerNotes[e.pointerId] === undefined || !(e.buttons & 1)) return;
      var el = e.target.closest(".piano-key");
      if (!el) return;
      var note = base + parseInt(el.dataset.idx, 10);
      if (note === pointerNotes[e.pointerId]) return;
      noteOff(pointerNotes[e.pointerId]);
      pointerNotes[e.pointerId] = note;
      noteOn(note, velFromEvent(e, el));
    });

    function releasePointer(e) {
      if (pointerNotes[e.pointerId] === undefined) return;
      noteOff(pointerNotes[e.pointerId]);
      delete pointerNotes[e.pointerId];
    }
    document.addEventListener("pointerup", releasePointer);
    document.addEventListener("pointercancel", releasePointer);

    // -- computer keyboard: home row whites, row above blacks --

    var KEYMAP = {
      a: 0, w: 1, s: 2, e: 3, d: 4, f: 5, t: 6, g: 7, y: 8, h: 9, u: 10, j: 11,
      k: 12, o: 13, l: 14, p: 15, ";": 16,
    };
    var kbdHeld = {}; // key char -> sounding MIDI note (pitch fixed at press
                      // time, so an octave shift mid-hold can't strand a note)

    function isTypingTarget(e) {
      var t = e.target;
      return t && (t.tagName === "INPUT" || t.tagName === "SELECT" || t.tagName === "TEXTAREA");
    }

    document.addEventListener("keydown", function (e) {
      if (e.repeat || e.metaKey || e.ctrlKey || e.altKey || isTypingTarget(e)) return;
      var k = e.key.toLowerCase();
      if (k === " ") {
        // preventDefault stops page scroll and re-clicking a focused button.
        e.preventDefault();
        return pedalSet(true);
      }
      if (k === "z") return setBase(base - 12);
      if (k === "x") return setBase(base + 12);
      if (!(k in KEYMAP) || kbdHeld[k] !== undefined) return;
      var note = base + KEYMAP[k];
      kbdHeld[k] = note;
      noteOn(note, 0.8);
    });

    document.addEventListener("keyup", function (e) {
      var k = e.key.toLowerCase();
      if (k === " ") return pedalSet(false);
      if (kbdHeld[k] === undefined) return;
      noteOff(kbdHeld[k]);
      delete kbdHeld[k];
    });

    document.getElementById("piano-oct-down").addEventListener("click", function () {
      setBase(base - 12);
    });
    document.getElementById("piano-oct-up").addEventListener("click", function () {
      setBase(base + 12);
    });

    // Picking a channel that isn't running a synth installs one (structural
    // change → reload, per the page's single-source-of-truth convention).
    if (pianoSel) pianoSel.addEventListener("change", function () {
      var opt = pianoSel.options[pianoSel.selectedIndex];
      if (opt && opt.dataset.synth === "1") return;
      post("/api/channel/" + pianoSel.value + "/source", { type: "synth" }).then(function (r) {
        if (r.ok) location.reload();
      });
    });

    // -- Web MIDI: a hardware keyboard is just another producer of the same
    // noteOn/noteOff calls the on-screen keys make, so velocity mapping, key
    // lighting, and channel routing are all shared. Notes deliberately skip
    // debounced() for the same reason on-screen notes do. --

    var midiBox = document.getElementById("midi-box");
    var midiDot = document.getElementById("midi-dot");
    var midiSel = document.getElementById("midi-in");

    if (!navigator.requestMIDIAccess) {
      // No Web MIDI (e.g. Safari): hide the shell rather than show a dead
      // control. The rest of the piano works as before.
      if (midiBox) midiBox.style.display = "none";
    } else if (midiSel) {
      var midiInput = null; // the MIDIInput currently feeding the synth
      var midiHeld = {};    // note -> true; lets rebind/unplug release notes

      // Release everything this device is sounding. Called before any
      // rebind or on device loss — otherwise a note held across an unplug
      // would ring until voice-steal claimed it (or forever, pre-decay).
      function midiPanic() {
        Object.keys(midiHeld).forEach(function (n) { noteOff(parseInt(n, 10)); });
        midiHeld = {};
        pedalSet(false); // an unplugged pedal can never send its own release
      }

      function onMIDIMessage(e) {
        var status = e.data[0] & 0xf0, note = e.data[1], vel = e.data[2];
        // Listen omni (low nibble = MIDI channel, ignored): a single-synth
        // target has no use for channel filtering yet. Many keyboards send
        // note-on velocity 0 instead of a real note-off (running-status
        // optimization), so both spellings must release the note.
        if (status === 0x90 && vel > 0) {
          midiHeld[note] = true;
          noteOn(note, vel / 127);
        } else if (status === 0x80 || (status === 0x90 && vel === 0)) {
          if (midiHeld[note]) {
            delete midiHeld[note];
            noteOff(note);
          }
        } else if (status === 0xb0 && note === 64) {
          // CC64 sustain: hardware sends a continuous 0..127; ≥64 is the
          // MIDI-standard "down" threshold. pedalSet dedupes the stream.
          pedalSet(vel >= 64);
        }
      }

      function bindInput(access, id) {
        midiPanic();
        if (midiInput) midiInput.onmidimessage = null;
        midiInput = id ? access.inputs.get(id) || null : null;
        if (midiInput) midiInput.onmidimessage = onMIDIMessage;
        midiDot.dataset.live = midiInput ? "1" : "0";
        midiDot.title = midiInput ? "MIDI: " + midiInput.name : "MIDI: no device";
        try { localStorage.setItem("midi-in", id || ""); } catch (_) {}
      }

      // Rebuild the device list. Selection preference: what's currently
      // bound, then the remembered device, then the first available — so
      // plugging in a keyboard just works on a fresh profile.
      function refreshInputs(access) {
        var want = (midiInput && midiInput.id) || (function () {
          try { return localStorage.getItem("midi-in") || ""; } catch (_) { return ""; }
        })();
        midiSel.innerHTML = "";
        var none = document.createElement("option");
        none.value = "";
        none.textContent = "MIDI: none";
        midiSel.appendChild(none);
        access.inputs.forEach(function (inp) {
          var o = document.createElement("option");
          o.value = inp.id;
          o.textContent = inp.name || inp.id;
          midiSel.appendChild(o);
        });
        if (want && access.inputs.get(want)) midiSel.value = want;
        else if (access.inputs.size > 0) midiSel.value = access.inputs.keys().next().value;
        bindInput(access, midiSel.value);
      }

      navigator.requestMIDIAccess().then(function (access) {
        refreshInputs(access);
        // statechange covers hot-plug both ways; a full refresh (rather than
        // incremental patching) keeps the list and binding trivially
        // consistent, and midiPanic() inside bindInput prevents stuck notes
        // when the bound device is the one that vanished.
        access.onstatechange = function () { refreshInputs(access); };
        midiSel.addEventListener("change", function () { bindInput(access, midiSel.value); });
      }, function () {
        // Permission denied: keep the shell visible but inert so the user
        // can see why a plugged-in keyboard is silent.
        midiDot.dataset.live = "err";
        midiDot.title = "MIDI access blocked by the browser";
        midiSel.disabled = true;
        midiSel.options[0].textContent = "MIDI blocked";
      });
    }

    // -- tutorial: guided lessons over the same noteOn/noteOff plumbing --
    // The server owns the lesson catalog (/api/lessons); this block owns all
    // interaction. Amber guide rings mark the next step's keys, any input
    // route can answer (they all funnel through noteOn), chord steps advance
    // only when the full set is held at once, and Listen replays the lesson
    // through the synth by driving the very same noteOn/noteOff as live play.

    var tutPanel = document.getElementById("tutorial");
    var tutSel = document.getElementById("tut-lesson");
    var tutStrip = document.getElementById("tut-strip");
    var tutProg = document.getElementById("tut-progress");
    var tutMsg = document.getElementById("tut-msg");

    var tutLessons = [];   // catalog fetched once at page load
    var tutActive = false; // when true, tutNoteOn checks input against steps
    var tutStep = 0, tutMisses = 0;
    var tutHeld = {};      // note -> true; lets chord steps require a full hold
    var demoOn = false;    // demo notes replay through noteOn; this flag keeps
                           // them (and stray play during a demo) out of the checker
    var demoTimers = [], demoSounding = {};

    function tutLesson() {
      return tutSel ? tutLessons[parseInt(tutSel.value, 10)] || null : null;
    }

    // Input checker, fed by noteOn. A note outside the current step counts as
    // a miss (red flash); a step completes when every one of its notes is
    // down simultaneously — which for melody steps is just the one note.
    // Checking only on note-on means holding a common tone across two chords
    // is fine, exactly as it is on a real piano.
    function tutNoteOn(note) {
      if (!tutActive || demoOn) return;
      tutHeld[note] = true;
      var lesson = tutLesson();
      var step = lesson && lesson.steps[tutStep];
      if (!step) return;
      if (step.notes.indexOf(note) === -1) {
        tutMisses++;
        flashMiss(note);
        paintTutProgress();
        return;
      }
      var complete = step.notes.every(function (n) { return tutHeld[n]; });
      if (complete) tutAdvance();
    }

    function tutNoteOff(note) { delete tutHeld[note]; }

    function keyEl(note) {
      var idx = note - base;
      if (idx < 0 || idx > 24) return null;
      return pianoKeys.querySelector('.piano-key[data-idx="' + idx + '"]');
    }

    function clearGuides() {
      pianoKeys.querySelectorAll('.piano-key[data-guide="1"]').forEach(function (k) {
        k.removeAttribute("data-guide");
      });
    }

    function guideStep() {
      clearGuides();
      var lesson = tutLesson();
      var step = tutActive && lesson && lesson.steps[tutStep];
      if (!step) return;
      step.notes.forEach(function (n) {
        var el = keyEl(n);
        if (el) el.dataset.guide = "1";
      });
    }

    function flashMiss(note) {
      var el = keyEl(note);
      if (!el) return;
      el.dataset.miss = "1";
      setTimeout(function () { el.removeAttribute("data-miss"); }, 250);
    }

    function buildStrip(lesson) {
      tutStrip.innerHTML = "";
      lesson.steps.forEach(function (s, i) {
        var chip = document.createElement("span");
        chip.className = "tut-chip";
        chip.dataset.i = String(i);
        chip.textContent = s.label;
        tutStrip.appendChild(chip);
      });
    }

    // Mark chips before cur done, cur current, the rest pending; cur = -1
    // clears everything. Steps.length marks the whole strip done.
    function stripCursor(cur) {
      tutStrip.querySelectorAll(".tut-chip").forEach(function (chip) {
        var i = parseInt(chip.dataset.i, 10);
        if (i < cur) {
          chip.dataset.done = "1";
          chip.removeAttribute("data-cur");
        } else if (i === cur) {
          chip.dataset.cur = "1";
          chip.removeAttribute("data-done");
          chip.scrollIntoView({ block: "nearest", inline: "center" });
        } else {
          chip.removeAttribute("data-done");
          chip.removeAttribute("data-cur");
        }
      });
    }

    function paintTutProgress() {
      var lesson = tutLesson();
      if (!lesson) { tutProg.textContent = ""; return; }
      var text = Math.min(tutStep + 1, lesson.steps.length) + "/" + lesson.steps.length;
      if (tutMisses) text += " · " + tutMisses + " missed";
      tutProg.textContent = text;
    }

    // Shift the keyboard so every lesson note is visible: base goes to the C
    // at or below the lesson's lowest note (lessons are authored to span at
    // most the two visible octaves from there).
    function fitBase(lesson) {
      var min = Infinity;
      lesson.steps.forEach(function (s) {
        s.notes.forEach(function (n) { min = Math.min(min, n); });
      });
      if (isFinite(min)) setBase(Math.floor(min / 12) * 12);
    }

    function startLesson() {
      var lesson = tutLesson();
      if (!lesson) return;
      stopDemo();
      tutActive = true;
      tutStep = 0;
      tutMisses = 0;
      tutHeld = {};
      fitBase(lesson);
      buildStrip(lesson);
      stripCursor(0);
      guideStep();
      paintTutProgress();
      tutMsg.textContent = lesson.steps[0].notes.length > 1
        ? "Hold every ringed key at once"
        : "Play the ringed key";
    }

    function tutAdvance() {
      var lesson = tutLesson();
      tutStep++;
      if (tutStep >= lesson.steps.length) {
        tutActive = false;
        clearGuides();
        stripCursor(lesson.steps.length);
        paintTutProgress();
        tutMsg.textContent = tutMisses === 0
          ? "Lesson complete — flawless!"
          : "Lesson complete — " + tutMisses + " missed. Try again?";
        return;
      }
      stripCursor(tutStep);
      guideStep();
      paintTutProgress();
    }

    function stopDemo() {
      demoTimers.forEach(clearTimeout);
      demoTimers = [];
      demoOn = false;
      Object.keys(demoSounding).forEach(function (n) { noteOff(parseInt(n, 10)); });
      demoSounding = {};
    }

    // Listen mode: schedule the whole lesson up front with plain timeouts.
    // Timing is display-grade, not sequencer-grade — fine for "hear how it
    // goes". Notes release slightly early so repeats re-strike audibly.
    function playDemo() {
      var lesson = tutLesson();
      if (!lesson) return;
      stopDemo();
      tutActive = false; // Listen is its own mode; Start re-arms checking
      clearGuides();
      fitBase(lesson);
      buildStrip(lesson);
      demoOn = true;
      tutMsg.textContent = "Listen…";
      var t = 0;
      lesson.steps.forEach(function (s, i) {
        var dur = s.ms || 400;
        demoTimers.push(setTimeout(function () {
          stripCursor(i);
          s.notes.forEach(function (n) { demoSounding[n] = true; noteOn(n, 0.7); });
        }, t));
        demoTimers.push(setTimeout(function () {
          s.notes.forEach(function (n) { delete demoSounding[n]; noteOff(n); });
        }, t + dur - 80));
        t += dur;
      });
      demoTimers.push(setTimeout(function () {
        demoOn = false;
        stripCursor(-1);
        tutMsg.textContent = lesson.desc + " Press Start to try it.";
      }, t));
    }

    function stopTutorial() {
      tutActive = false;
      stopDemo();
      clearGuides();
      previewLesson();
    }

    // Idle view of the selected lesson: its chips and description, no cursor.
    function previewLesson() {
      var lesson = tutLesson();
      if (!lesson) return;
      buildStrip(lesson);
      tutMsg.textContent = lesson.desc;
      tutProg.textContent = "";
    }

    if (tutPanel && tutSel) {
      fetch("/api/lessons").then(function (r) { return r.json(); }).then(function (d) {
        tutLessons = d || [];
        previewLesson();
      }).catch(function (e) { console.error("lessons", e); });

      tutSel.addEventListener("change", function () {
        tutActive = false;
        stopDemo();
        clearGuides();
        previewLesson();
      });
      document.getElementById("tut-start").addEventListener("click", startLesson);
      document.getElementById("tut-demo").addEventListener("click", playDemo);
      document.getElementById("tut-stop").addEventListener("click", stopTutorial);
    }
  }

  // ---- meters via SSE ------------------------------------------------------

  // Map a linear level onto meter height through dB: (db+60)/60 puts -60dB
  // at the bottom and 0dBFS at the top, matching how console meters read.
  function levelPct(lin) {
    if (lin <= 0.000001) return 0;
    var db = 20 * Math.log10(lin);
    return Math.max(0, Math.min(1, (db + 60) / 60)) * 100;
  }

  function paintMeter(key, peak, rms) {
    var m = document.querySelector('.meter[data-meter="' + key + '"]');
    if (!m) return;
    m.querySelector(".meter-rms").style.height = levelPct(rms) + "%";
    m.querySelector(".meter-peak").style.bottom = levelPct(peak) + "%";
  }

  var es = new EventSource("/events");
  var connDot = document.getElementById("conn-dot");

  es.onopen = function () { if (connDot) connDot.dataset.live = "1"; };
  es.onerror = function () { if (connDot) connDot.dataset.live = "0"; };

  es.onmessage = function (e) {
    var msg;
    try { msg = JSON.parse(e.data); } catch (_) { return; }
    if (msg.type !== "meters" || !msg.data) return;
    var d = msg.data;

    (d.ch || []).forEach(function (m, i) { paintMeter("ch-" + (i + 1), Math.max(m.pl, m.pr), Math.max(m.rl, m.rr)); });
    (d.grp || []).forEach(function (m, i) { paintMeter("grp-" + (i + 1), Math.max(m.pl, m.pr), Math.max(m.rl, m.rr)); });
    if (d.master) paintMeter("master", Math.max(d.master.pl, d.master.pr), Math.max(d.master.rl, d.master.rr));

    var recBtn = document.getElementById("rec-btn");
    var recTime = document.getElementById("rec-time");
    if (recBtn) recBtn.dataset.on = d.recording ? "1" : "0";
    if (recTime) recTime.textContent = d.recording ? d.recSeconds.toFixed(1) + "s" : "";

    // MIDI transport readouts ride the same broadcast (entries align with
    // channel index; null = channel has no player).
    (d.midi || []).forEach(function (mi, i) {
      if (!mi) return;
      var id = i + 1;
      var pos = document.querySelector('.midi-pos[data-id="' + id + '"]');
      if (pos) pos.textContent = clock(mi.pos) + " / " + clock(mi.len);
      var play = document.querySelector('button[data-role="midi-play"][data-id="' + id + '"]');
      var pause = document.querySelector('button[data-role="midi-pause"][data-id="' + id + '"]');
      if (play) play.dataset.state = mi.state === "playing" ? "playing" : "";
      if (pause) pause.dataset.state = mi.state === "paused" ? "paused" : "";
    });
  };

  // clock formats seconds as m:ss, mirroring the server's initial render.
  function clock(sec) {
    var s = Math.max(0, Math.floor(sec));
    return Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
  }
})();
