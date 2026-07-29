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
    var wav = document.querySelector('.src-wav[data-id="' + id + '"]');
    if (osc) osc.dataset.visible = type === "osc" ? "1" : "0";
    if (wav) wav.dataset.visible = type === "wav" ? "1" : "0";
  }

  // Initialize source sub-row visibility from the current selects.
  document.querySelectorAll('select[data-role="source-select"]').forEach(function (sel) {
    showSourceRows(sel.dataset.id, sel.value);
  });

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
