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
      post("/api/channel/" + id + "/source", { type: type }).then(function (r) {
        if (r.ok) location.reload();
      });
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
    var wav = document.querySelector('.src-wav[data-id="' + id + '"]');
    if (osc) osc.dataset.visible = type === "osc" ? "1" : "0";
    if (syn) syn.dataset.visible = type === "synth" ? "1" : "0";
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

    function noteOn(note, vel) {
      light(note, true);
      post("/api/channel/" + pianoCh() + "/note", { note: note, on: true, velocity: vel });
    }
    function noteOff(note) {
      light(note, false);
      post("/api/channel/" + pianoCh() + "/note", { note: note, on: false });
    }

    function setBase(nb) {
      base = Math.max(BASE_MIN, Math.min(BASE_MAX, nb));
      if (octLabel) octLabel.textContent = "C" + (base / 12 - 1);
      // Clear highlights: lit keys now show different pitches. Held notes
      // still release correctly because note-offs use the stored pitch.
      pianoKeys.querySelectorAll('.piano-key[data-on="1"]').forEach(function (k) {
        k.dataset.on = "0";
      });
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
      if (k === "z") return setBase(base - 12);
      if (k === "x") return setBase(base + 12);
      if (!(k in KEYMAP) || kbdHeld[k] !== undefined) return;
      var note = base + KEYMAP[k];
      kbdHeld[k] = note;
      noteOn(note, 0.8);
    });

    document.addEventListener("keyup", function (e) {
      var k = e.key.toLowerCase();
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
  };
})();
