// The central dashboard (#99). ficha.js does the live part (the stream, the
// reconcile by data-key, the tickers, the track and the player); this adds
// what only the central has, with no request outside this server:
// - the theme: dark by default, the toggle remembered on this screen;
// - the wall clock in Caracas time;
// - the rows sliding in on load;
// - the transcript, timeline and points following their newest entry;
// - ?seguir=1 on the list: opens a new case by itself (the presenter is on
//   stage holding the phone);
// - the alarm sound (#143): a short siren when an alarm fires on screen,
//   with a mute toggle remembered on this screen.
(function () {
  "use strict";

  var root = document.documentElement;
  var body = document.body;
  var motion = !(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
  var KEY = "mama-central-tema";

  // ------------------------------------------------------------ theme

  function store(v) { try { if (v) { localStorage.setItem(KEY, v); } return localStorage.getItem(KEY); } catch (e) { return null; } }
  function setTheme(t) {
    root.setAttribute("data-theme", t);
    var b = document.querySelector("[data-theme-toggle]");
    if (b) { b.setAttribute("aria-label", t === "dark" ? "Cambiar a modo claro" : "Cambiar a modo oscuro"); }
  }
  var asked = /[?&]tema=/.test(window.location.search);
  var saved = store();
  if (!asked && (saved === "dark" || saved === "light")) { setTheme(saved); } else { setTheme(root.getAttribute("data-theme") || "dark"); }
  document.addEventListener("click", function (ev) {
    if (!ev.target.closest || !ev.target.closest("[data-theme-toggle]")) { return; }
    var t = root.getAttribute("data-theme") === "dark" ? "light" : "dark";
    setTheme(t);
    store(t);
  });

  // ------------------------------------------------------------ wall clock

  var fmt = null;
  try { fmt = new Intl.DateTimeFormat("es-VE", { timeZone: "America/Caracas", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }); } catch (e) { fmt = null; }
  function wall() {
    var el = document.querySelector("[data-now]");
    if (!el) { return; }
    var d = new Date();
    if (fmt) { el.textContent = fmt.format(d); return; }
    var v = new Date(d.getTime() - 4 * 3600 * 1000); // VET is UTC-4, no DST
    el.textContent = v.toISOString().slice(11, 19);
  }

  // ------------------------------------------------------------ rows

  function staggerRows() {
    if (!motion) { return; }
    Array.prototype.forEach.call(document.querySelectorAll(".c-row, .c-count"), function (n, i) {
      n.style.setProperty("--i", String(Math.min(i, 12)));
      n.classList.add("enter");
      n.addEventListener("animationend", function done(e) { if (e.target === n) { n.classList.remove("enter"); n.removeEventListener("animationend", done); } });
    });
  }

  // follow opens a case that arrives while the list is open, after a beat so
  // the room sees it slide in.
  function follow(list) {
    var gone = false;
    new MutationObserver(function (records) {
      if (gone) { return; }
      records.forEach(function (r) {
        Array.prototype.forEach.call(r.addedNodes, function (n) {
          if (gone || n.nodeType !== 1 || !n.hasAttribute("data-new") || !n.getAttribute("href")) { return; }
          gone = true;
          setTimeout(function () { window.location.assign(n.getAttribute("href")); }, 2600);
        });
      });
    }).observe(list, { childList: true });
  }

  // ------------------------------------------------------------ following the newest entry

  // stick keeps a scroll box at its end when new content arrives, unless the
  // viewer scrolled up to read.
  function stick(box, pick) {
    if (!box) { return; }
    var atEnd = true;
    box.addEventListener("scroll", function () { atEnd = box.scrollHeight - box.scrollTop - box.clientHeight < 48; }, { passive: true });
    var go = function () {
      var b = pick ? pick() : box;
      if (b && (atEnd || b !== box)) { b.scrollTo({ top: b.scrollHeight, behavior: motion ? "smooth" : "auto" }); }
    };
    // Only new elements count: the tickers replace text nodes ten times a
    // second.
    new MutationObserver(function (records) {
      for (var i = 0; i < records.length; i++) {
        for (var j = 0; j < records[i].addedNodes.length; j++) {
          if (records[i].addedNodes[j].nodeType === 1) { go(); return; }
        }
      }
    }).observe(box, { childList: true, subtree: true });
    go();
  }

  // ------------------------------------------------------------ alarm sound (#143)

  // A short siren when an alarm fires while the screen is open: the detail's
  // chip turning red, or a list row turning into an alarm. Browsers allow
  // sound only after the viewer touched the page, so it stays quiet until
  // then; it never plays over a recording that is playing, nor once muted.
  var SOUND = "mama-central-alarma";
  var audio = null, touched = false, seen = {};

  function soundOn() { try { return localStorage.getItem(SOUND) !== "off"; } catch (e) { return true; } }
  function setSound(on) {
    try { if (on) { localStorage.removeItem(SOUND); } else { localStorage.setItem(SOUND, "off"); } } catch (e) { /* storage blocked: this view only */ }
    Array.prototype.forEach.call(document.querySelectorAll("[data-alarm-sound]"), function (b) {
      b.setAttribute("aria-pressed", on ? "true" : "false");
      b.setAttribute("aria-label", on ? "Silenciar la alarma" : "Activar el sonido de la alarma");
    });
    soundState = on;
  }
  var soundState = soundOn();

  // alarms are the list's red rows, by key. The detail's chip turning red
  // comes from ficha.js as a "ficha:alarm" event, with its flash.
  function alarms() {
    var now = {};
    Array.prototype.forEach.call(document.querySelectorAll("[data-alarm]"), function (r) {
      now[r.getAttribute("data-alarm")] = true;
    });
    return now;
  }

  function recordingPlays() {
    return Array.prototype.some.call(document.querySelectorAll("audio"), function (a) { return !a.paused; });
  }

  // siren plays three falling two-tone beeps, about a second and a half.
  function siren() {
    if (!soundState || !touched || recordingPlays()) { return; }
    var Ctx = window.AudioContext || window.webkitAudioContext;
    if (!Ctx) { return; }
    try {
      audio = audio || new Ctx();
      if (audio.resume) { audio.resume(); }
      var t = audio.currentTime + 0.05;
      for (var i = 0; i < 3; i++) {
        [[988, 0], [740, 0.22]].forEach(function (n) {
          var o = audio.createOscillator(), g = audio.createGain();
          o.type = "square";
          o.frequency.value = n[0];
          var at = t + i * 0.5 + n[1];
          g.gain.setValueAtTime(0.0001, at);
          g.gain.exponentialRampToValueAtTime(0.06, at + 0.02);
          g.gain.exponentialRampToValueAtTime(0.0001, at + 0.2);
          o.connect(g).connect(audio.destination);
          o.start(at);
          o.stop(at + 0.21);
        });
      }
    } catch (e) { /* no sound: the chip still shows */ }
  }

  function watchAlarms() {
    seen = alarms(); // what was already on screen when it opened makes no sound
    document.addEventListener("ficha:update", function () {
      var now = alarms(), fired = false;
      Object.keys(now).forEach(function (k) { if (!seen[k]) { fired = true; } seen[k] = true; });
      if (fired) { siren(); }
    });
    document.addEventListener("ficha:alarm", siren);
    var touch = function () { touched = true; };
    document.addEventListener("pointerdown", touch, { once: true, capture: true });
    document.addEventListener("keydown", touch, { once: true, capture: true });
    setSound(soundState);
    document.addEventListener("click", function (ev) {
      if (!ev.target.closest || !ev.target.closest("[data-alarm-sound]")) { return; }
      setSound(!soundState);
    });
  }

  function start() {
    wall();
    setInterval(wall, 1000);
    watchAlarms();
    var list = document.querySelector('[data-live="c-list"]');
    if (list) {
      staggerRows();
      if (body.hasAttribute("data-follow")) { follow(list); }
    }
    Array.prototype.forEach.call(document.querySelectorAll("[data-autoscroll]"), function (b) { stick(b); });
    var call = document.querySelector('.panel-call [data-live="call"]');
    if (call) { stick(call, function () { return call.querySelector(".turns") || call; }); }
  }
  if (document.readyState === "loading") { document.addEventListener("DOMContentLoaded", start); } else { start(); }
})();
