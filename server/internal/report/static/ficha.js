// The incident report page (#76), with no dependencies and no request outside
// this server.
//
// - Live: an EventSource on /i/{token}/live (polling /estado.json when
//   streaming fails) brings server-rendered sections; reconcile() keeps every
//   element whose data-key and HTML did not change and animates the new ones.
// - Tickers: [data-since] elements count up from an instant, corrected by the
//   server's clock.
// - The call: a waveform drawn from the recording with the Web Audio API,
//   tap-to-seek on the waveform and on each turn, and the playing turn
//   highlighted.
// - The demo page (/demo/next) waits for the next incident and opens it.
(function () {
  "use strict";

  var body = document.body;
  var motion = !(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
  var skew = 0; // server clock minus this browser's, in ms

  // ------------------------------------------------------------ reconcile

  // snapshot remembers each keyed element's HTML as the server sent it, so a
  // later update can tell what changed even after the page added classes.
  function snapshot(root) {
    Array.prototype.forEach.call(root.querySelectorAll("[data-key]"), function (n) { n._html = n.outerHTML; });
  }

  function keyOf(n, i) { return (n.getAttribute && n.getAttribute("data-key")) || "#" + i; }

  function animateIn(n, i) {
    if (!motion || n.nodeType !== 1) { return; }
    var cls = n.hasAttribute("data-reveal") ? "reveal" : "enter";
    if (n.style && n.style.setProperty) { n.style.setProperty("--i", String(i)); }
    n.classList.add(cls);
    n.addEventListener("animationend", function done(e) {
      if (e.target !== n) { return; }
      n.classList.remove(cls);
      n.removeEventListener("animationend", done);
    });
  }

  // reconcile makes container's element children match html: same key and
  // same HTML is kept as it is (a playing audio element survives), same key
  // with data-live-children is updated child by child, anything else is
  // replaced or inserted with an entrance. Nodes already in place are never
  // moved, so their animations do not restart.
  function reconcile(container, html) {
    var tpl = document.createElement("template");
    tpl.innerHTML = html;
    var next = Array.prototype.slice.call(tpl.content.children);
    patchChildren(container, next);
  }

  function patchChildren(parent, next) {
    var old = {};
    Array.prototype.forEach.call(parent.children, function (n, i) { old[keyOf(n, i)] = n; });
    var fresh = 0;
    var wanted = next.map(function (n, i) {
      var k = keyOf(n, i);
      var o = old[k];
      delete old[k];
      var html = n.outerHTML;
      if (o && o._html === html) { return o; }
      if (o && n.hasAttribute("data-live-children") && o.hasAttribute("data-live-children")) {
        Array.prototype.slice.call(o.attributes).forEach(function (a) { if (!n.hasAttribute(a.name) && a.name !== "style") { o.removeAttribute(a.name); } });
        Array.prototype.forEach.call(n.attributes, function (a) { o.setAttribute(a.name, a.value); });
        patchChildren(o, Array.prototype.slice.call(n.children));
        o._html = html;
        return o;
      }
      n._html = html;
      snapshot(n);
      animateIn(n, fresh++);
      return n;
    });
    Object.keys(old).forEach(function (k) { old[k].remove(); });
    var cur = parent.firstElementChild;
    wanted.forEach(function (n) {
      if (n === cur) { cur = cur.nextElementSibling; return; }
      parent.insertBefore(n, cur);
    });
  }

  // ------------------------------------------------------------ live state

  var badge = document.querySelector("[data-live-badge]");
  function setBadge(mode) {
    if (!badge) { return; }
    var text = badge.querySelector("[data-live-text]");
    badge.hidden = mode === "off";
    badge.classList.toggle("reconnecting", mode === "reconnecting");
    if (text) { text.textContent = mode === "reconnecting" ? "Reconectando" : "En vivo"; }
  }

  var version = "";
  function apply(s) {
    if (s.server_now) { skew = Date.parse(s.server_now) - Date.now(); }
    if (s.version === version) { return; }
    version = s.version;
    Object.keys(s.sections || {}).forEach(function (name) {
      var c = document.querySelector('[data-live="' + name + '"]');
      if (c) { reconcile(c, s.sections[name]); }
    });
    body.setAttribute("data-state", s.state);
    body.className = body.className.replace(/\bstate-\w+/, "state-" + s.state);
    if (s.live) { body.setAttribute("data-open", ""); } else { body.removeAttribute("data-open"); }
    afterUpdate();
  }

  function startLive() {
    var url = body.getAttribute("data-live-url");
    if (!url) { return; }
    if (!body.hasAttribute("data-open")) { return; } // sealed: nothing will change
    if (!window.EventSource) { poll(); return; }
    var es = new EventSource(url);
    var failures = 0;
    es.addEventListener("open", function () { failures = 0; setBadge("live"); });
    es.addEventListener("update", function (e) { failures = 0; setBadge("live"); apply(JSON.parse(e.data)); });
    es.addEventListener("end", function () { es.close(); setBadge("off"); });
    es.addEventListener("error", function () {
      failures++;
      setBadge("reconnecting");
      // EventSource retries by itself; give up on it after a few failures
      // in a row (a proxy that buffers or blocks streams) and poll instead.
      if (failures >= 4 || es.readyState === EventSource.CLOSED) { es.close(); poll(); }
    });
  }

  var polling = false;
  function poll() {
    if (polling) { return; }
    polling = true;
    var url = body.getAttribute("data-json-url");
    function tick() {
      fetch(url, { cache: "no-store", credentials: "same-origin" })
        .then(function (r) { if (!r.ok) { throw new Error(String(r.status)); } return r.json(); })
        .then(function (s) {
          setBadge("live");
          apply(s);
          if (s.end) { setBadge("off"); return; }
          setTimeout(tick, 2000);
        })
        .catch(function () { setBadge("reconnecting"); setTimeout(tick, 4000); });
    }
    tick();
  }

  // ------------------------------------------------------------ tickers

  function pad(n) { return n < 10 ? "0" + n : String(n); }
  function format(ms, kind) {
    if (ms < 0) { ms = 0; }
    var s = ms / 1000;
    if (kind === "latency") {
      if (s < 60) { return s.toFixed(1).replace(".", ",") + " s"; }
      return Math.floor(s / 60) + " min " + Math.floor(s % 60) + " s";
    }
    var h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.floor(s % 60);
    var tenths = Math.floor((s * 10) % 10);
    return (h ? h + ":" + pad(m) : String(m)) + ":" + pad(sec) + (h ? "" : "," + tenths);
  }
  function tick() {
    var now = Date.now() + skew;
    Array.prototype.forEach.call(document.querySelectorAll("[data-since]"), function (el) {
      var t = Date.parse(el.getAttribute("data-since"));
      if (!isNaN(t)) { el.textContent = format(now - t, el.getAttribute("data-format")); }
    });
  }

  // ------------------------------------------------------------ timeline

  function afterUpdate() {
    var list = document.querySelector('[data-live="timeline"]');
    var fill = document.querySelector("[data-rail]");
    if (list) {
      var items = list.children;
      Array.prototype.forEach.call(items, function (li) { li.classList.remove("now"); });
      var last = items[items.length - 1];
      if (last && body.hasAttribute("data-open")) { last.classList.add("now"); }
      if (fill && last) {
        // The rail starts 8px down; the last dot's center is 14px into its item.
        fill.style.height = body.hasAttribute("data-open") ? (last.offsetTop + 6) + "px" : "100%";
      }
    }
    initPlayers();
    initMarks();
  }

  // ------------------------------------------------------------ the call

  function audioOf(call) { return document.getElementById("audio-" + call); }

  function seek(audio, secs) {
    var go = function () {
      try { audio.currentTime = secs; } catch (e) { /* not seekable yet */ }
      var p = audio.play();
      if (p && p.catch) { p.catch(function () {}); }
    };
    if (audio.readyState >= 1) { go(); } else {
      audio.addEventListener("loadedmetadata", go, { once: true });
      audio.load();
    }
  }

  document.addEventListener("click", function (ev) {
    var el = ev.target.closest && ev.target.closest("[data-seek]");
    if (!el) { return; }
    var audio = audioOf(el.getAttribute("data-call"));
    if (!audio) { return; }
    ev.preventDefault();
    if (el.hasAttribute("data-scroll")) {
      var card = document.getElementById("llamada-" + el.getAttribute("data-call"));
      if (card) { card.scrollIntoView({ behavior: motion ? "smooth" : "auto", block: "start" }); }
    }
    seek(audio, parseFloat(el.getAttribute("data-seek")) || 0);
  });

  // initMarks highlights, for each call, the turn that is playing.
  function initMarks() {
    Array.prototype.forEach.call(document.querySelectorAll("audio[data-call]"), function (audio) {
      if (audio._marks) { return; }
      audio._marks = true;
      var call = audio.getAttribute("data-call");
      var current = null;
      function turns() { return document.querySelectorAll('.turn[data-call="' + call + '"]'); }
      function mark() {
        var t = audio.currentTime, best = null;
        Array.prototype.forEach.call(turns(), function (el) {
          if ((parseFloat(el.getAttribute("data-seek")) || 0) <= t + 0.05) { best = el; }
        });
        if (audio.paused && audio.currentTime === 0) { best = null; }
        if (best === current) { return; }
        if (current) { current.classList.remove("playing"); current.removeAttribute("aria-current"); }
        if (best) { best.classList.add("playing"); best.setAttribute("aria-current", "true"); }
        current = best;
      }
      audio.addEventListener("timeupdate", mark);
      audio.addEventListener("seeked", mark);
      audio.addEventListener("ended", function () {
        if (current) { current.classList.remove("playing"); current.removeAttribute("aria-current"); }
        current = null;
      });
      audio.addEventListener("error", function () {
        var note = document.getElementById("audio-error-" + call);
        if (note && !audio._blob) { note.hidden = false; audio.hidden = true; }
      });
    });
  }

  // initPlayers draws each recording's waveform: it downloads the recording
  // once from this server, decodes it for the peaks, and hands the same bytes
  // to the player as a blob, so it is not downloaded twice.
  function initPlayers() {
    Array.prototype.forEach.call(document.querySelectorAll("[data-player]"), function (block) {
      if (block._wave) { return; }
      block._wave = true;
      var audio = block.querySelector("audio");
      var canvas = block.querySelector("canvas");
      var status = block.querySelector("[data-wave-status]");
      var wave = { peaks: null, duration: 0 };
      var Offline = window.OfflineAudioContext || window.webkitOfflineAudioContext;
      if (!audio || !canvas || !window.fetch || !Offline) { if (status) { status.textContent = "Forma de onda no disponible."; } return; }
      fetch(audio.getAttribute("src"), { credentials: "same-origin" })
        .then(function (r) {
          if (!r.ok) { throw new Error(String(r.status)); }
          var type = r.headers.get("Content-Type") || "audio/mpeg";
          return r.arrayBuffer().then(function (buf) { return { buf: buf, type: type }; });
        })
        .then(function (got) {
          if (audio.paused && audio.currentTime === 0) {
            audio._blob = URL.createObjectURL(new Blob([got.buf], { type: got.type }));
            audio.src = audio._blob;
          }
          var ctx = new Offline(1, 1, 8000);
          return new Promise(function (ok, fail) { ctx.decodeAudioData(got.buf.slice(0), ok, fail); });
        })
        .then(function (decoded) {
          wave.duration = decoded.duration;
          wave.data = decoded.getChannelData(0);
          block.querySelector(".wave").classList.add("ready");
          draw();
          window.addEventListener("resize", draw);
          var raf = 0;
          function loop() { draw(); raf = audio.paused ? 0 : requestAnimationFrame(loop); }
          audio.addEventListener("play", function () { if (!raf) { raf = requestAnimationFrame(loop); } });
          audio.addEventListener("seeked", draw);
          audio.addEventListener("timeupdate", function () { if (!raf) { draw(); } });
        })
        .catch(function () { if (status) { status.textContent = "Forma de onda no disponible."; } });

      function color(name) { return getComputedStyle(document.documentElement).getPropertyValue(name).trim(); }

      function draw() {
        if (!wave.data) { return; }
        var dpr = window.devicePixelRatio || 1;
        var w = canvas.clientWidth, h = canvas.clientHeight;
        if (!w || !h) { return; }
        if (canvas.width !== Math.round(w * dpr)) { canvas.width = Math.round(w * dpr); canvas.height = Math.round(h * dpr); }
        var g = canvas.getContext("2d");
        g.setTransform(dpr, 0, 0, dpr, 0, 0);
        g.clearRect(0, 0, w, h);
        var bar = 3, gap = 2, n = Math.max(1, Math.floor(w / (bar + gap)));
        if (!wave.peaks || wave.peaks.length !== n) { wave.peaks = peaks(wave.data, n); }
        var played = wave.duration ? audio.currentTime / wave.duration : 0;
        var on = color("--primary"), off = color("--outline-variant"), mid = h / 2 - 6;
        for (var i = 0; i < n; i++) {
          var a = Math.max(2, wave.peaks[i] * (mid - 4) * 2);
          g.fillStyle = i / n < played ? on : off;
          g.fillRect(i * (bar + gap), mid - a / 2, bar, a);
        }
        // Turn starts as ticks under the wave; the phrase turn stands out.
        Array.prototype.forEach.call(document.querySelectorAll('.turn[data-call="' + audio.getAttribute("data-call") + '"]'), function (el) {
          var x = (parseFloat(el.getAttribute("data-seek")) || 0) / wave.duration * w;
          var phrase = el.classList.contains("phrase");
          g.fillStyle = phrase ? on : color("--outline");
          g.fillRect(x, h - (phrase ? 12 : 8), phrase ? 4 : 2, phrase ? 12 : 8);
        });
        g.fillStyle = color("--on-background");
        g.fillRect(Math.min(w - 2, played * w), 0, 2, h);
      }

      canvas.addEventListener("click", function (e) {
        if (!wave.duration) { return; }
        var r = canvas.getBoundingClientRect();
        seek(audio, (e.clientX - r.left) / r.width * wave.duration);
      });
    });
  }

  function peaks(data, n) {
    var out = new Array(n), step = Math.max(1, Math.floor(data.length / n)), max = 0;
    for (var i = 0; i < n; i++) {
      var p = 0, end = Math.min(data.length, (i + 1) * step);
      for (var j = i * step; j < end; j += 4) { var v = Math.abs(data[j]); if (v > p) { p = v; } }
      out[i] = p;
      if (p > max) { max = p; }
    }
    for (var k = 0; k < n; k++) { out[k] = max ? out[k] / max : 0; }
    return out;
  }

  // ------------------------------------------------------------ demo wait

  function demoWait(url) {
    function check() {
      fetch(url, { cache: "no-store", credentials: "same-origin" })
        .then(function (r) { return r.json(); })
        .then(function (d) { if (d.url) { window.location.replace(d.url); } else { setTimeout(check, 1000); } })
        .catch(function () { setTimeout(check, 2000); });
    }
    check();
  }

  // ------------------------------------------------------------ start

  function start() {
    snapshot(document);
    if (body.hasAttribute("data-demo-wait")) { demoWait(body.getAttribute("data-demo-wait")); return; }
    var list = document.querySelector('[data-live="timeline"]');
    if (list) { Array.prototype.forEach.call(list.children, function (li, i) { animateIn(li, i); }); }
    Array.prototype.forEach.call(document.querySelectorAll('[data-live="latency"] > *, .turns > li'), function (n, i) { animateIn(n, i % 12); });
    afterUpdate();
    tick();
    setInterval(tick, 100);
    window.addEventListener("resize", afterUpdate);
    startLive();
  }
  if (document.readyState === "loading") { document.addEventListener("DOMContentLoaded", start); } else { start(); }
})();
