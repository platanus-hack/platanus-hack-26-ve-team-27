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
      if (o) { o.remove(); } // replaced: the new element takes its place
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

  function setBadge(mode) {
    var badge = document.querySelector("[data-live-badge]");
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
    if (s.live) { body.setAttribute("data-open", ""); } else { body.removeAttribute("data-open"); }
    afterUpdate();
    document.dispatchEvent(new CustomEvent("ficha:update")); // route-map.js redraws the map
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
          if (s.end || !s.live) { setBadge("off"); return; } // sealed or no longer live: nothing more will come
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
    if (kind === "ago") {
      if (s < 60) { return "hace " + Math.floor(s) + " s"; }
      if (s < 3600) { return "hace " + Math.floor(s / 60) + " min"; }
      return "hace " + Math.floor(s / 3600) + " h";
    }
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

  // ------------------------------------------------------------ track

  // afterUpdate fills the track's line up to the last step that happened,
  // half way into the step the case waits for, and brings that step into
  // view on a phone, where the track scrolls sideways.
  function afterUpdate() {
    var track = document.querySelector('[data-live="track"]');
    var fill = document.querySelector("[data-track-fill]");
    var scroller = document.querySelector("[data-track-scroll]");
    if (track && fill) {
      var nodes = Array.prototype.slice.call(track.children);
      var inner = track.parentNode;
      if (inner && inner.style) { inner.style.setProperty("--nodes", String(nodes.length || 1)); }
      var center = function (n) { return n.offsetLeft + n.offsetWidth / 2; };
      var last = -1, current = -1;
      nodes.forEach(function (n, i) {
        if (n.classList.contains("node-done") || n.classList.contains("node-skipped")) { last = i; }
        if (n.classList.contains("node-current")) { current = i; }
      });
      if (nodes.length > 1) {
        var start = center(nodes[0]);
        var to = last >= 0 ? center(nodes[last]) : start;
        if (current > last && last >= 0) { to += (center(nodes[current]) - to) * 0.5; }
        fill.style.width = Math.max(0, to - start) + "px";
      }
      var focus = nodes[current >= 0 ? current : Math.max(last, 0)];
      if (scroller && focus && scroller.scrollWidth > scroller.clientWidth + 1) {
        var left = focus.offsetLeft - (scroller.clientWidth - focus.offsetWidth) / 2;
        scroller.scrollTo({ left: Math.max(0, left), behavior: motion && scroller._seen ? "smooth" : "auto" });
        scroller._seen = true;
      }
    }
    // A live transcript (#143) follows its newest phrase where it scrolls.
    Array.prototype.forEach.call(document.querySelectorAll(".turns[data-provisional]"), function (t) {
      t.scrollTop = t.scrollHeight;
    });
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
    if (!ev.target.closest) { return; }
    var play = ev.target.closest("[data-play]");
    if (play) {
      var a = audioOf(play.getAttribute("data-play"));
      if (a) { if (a.paused) { seek(a, a.currentTime || 0); } else { a.pause(); } }
      return;
    }
    if (ev.target.closest("[data-expand]")) { body.setAttribute("data-expanded", ""); return; }
    var el = ev.target.closest("[data-seek]");
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

  // One recording at a time: playing one pauses every other on the page
  // and, through a BroadcastChannel, in the reader's other tabs of this
  // server (the ficha and the central of the same case side by side).
  var tabId = String(Math.random()).slice(2);
  var channel = null;
  try { channel = window.BroadcastChannel ? new BroadcastChannel("mama-audio") : null; } catch (e) { channel = null; }
  function pauseOthers(keep) {
    Array.prototype.forEach.call(document.querySelectorAll("audio"), function (a) { if (a !== keep && !a.paused) { a.pause(); } });
  }
  document.addEventListener("play", function (ev) { // play does not bubble: listen in the capture phase
    if (ev.target.tagName !== "AUDIO") { return; }
    pauseOthers(ev.target);
    if (channel) { channel.postMessage({ from: tabId }); }
  }, true);
  if (channel) { channel.onmessage = function (ev) { if (ev.data && ev.data.from !== tabId) { pauseOthers(null); } }; }

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
      function button() { return document.querySelector('[data-play="' + call + '"]'); }
      function readout() {
        var block = audio.closest("[data-player]");
        var t = block && block.querySelector("[data-ptime]");
        if (t) { t.textContent = format(audio.currentTime * 1000, "clock").replace(/,\d$/, ""); }
      }
      audio.addEventListener("play", function () { var b = button(); if (b) { b.classList.add("playing"); b.setAttribute("aria-label", "Pausar la llamada"); } });
      audio.addEventListener("pause", function () { var b = button(); if (b) { b.classList.remove("playing"); b.setAttribute("aria-label", "Reproducir la llamada"); } });
      audio.addEventListener("timeupdate", function () { mark(); readout(); });
      audio.addEventListener("seeked", function () { mark(); readout(); });
      audio.addEventListener("ended", function () {
        if (current) { current.classList.remove("playing"); current.removeAttribute("aria-current"); }
        current = null;
        var b = button();
        if (b) { b.classList.remove("playing"); }
      });
      audio.addEventListener("error", function () {
        var block = audio.closest("[data-player]");
        var st = block && block.querySelector("[data-wave-status]");
        if (st && !audio._blob) { st.textContent = "Audio no disponible"; }
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
          var t = block.querySelector("[data-ptime]");
          if (t) { t.textContent = format(decoded.duration * 1000, "clock").replace(/,\d$/, ""); }
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
        var on = color("--primary"), off = color("--outline"), mid = h / 2 - 3;
        for (var i = 0; i < n; i++) {
          var a = Math.max(2, wave.peaks[i] * (h - 14));
          g.fillStyle = i / n < played ? on : off;
          g.fillRect(i * (bar + gap), mid - a / 2, bar, a);
        }
        // Turn starts as ticks under the wave; the phrase turn stands out.
        Array.prototype.forEach.call(document.querySelectorAll('.turn[data-call="' + audio.getAttribute("data-call") + '"]'), function (el) {
          var x = (parseFloat(el.getAttribute("data-seek")) || 0) / wave.duration * w;
          var phrase = el.classList.contains("phrase");
          g.fillStyle = phrase ? on : color("--outline-variant");
          g.fillRect(x, h - (phrase ? 8 : 5), phrase ? 4 : 2, phrase ? 8 : 5);
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
    var track = document.querySelector('[data-live="track"]');
    if (track) { Array.prototype.forEach.call(track.children, function (li, i) { li.classList.add("enter"); li.style.setProperty("--i", String(i)); li.addEventListener("animationend", function () { li.classList.remove("enter"); }, { once: true }); }); }
    Array.prototype.forEach.call(document.querySelectorAll(".panel, .turns > li, .fam"), function (n, i) { animateIn(n, i % 10); });
    afterUpdate();
    tick();
    setInterval(tick, 100);
    window.addEventListener("resize", afterUpdate);
    startLive();
  }
  if (document.readyState === "loading") { document.addEventListener("DOMContentLoaded", start); } else { start(); }
})();
