// The central dashboard (#99). ficha.js does the live part (the stream, the
// reconcile by data-key, the tickers, the track and the player); this adds
// what only the central has, with no request outside this server:
// - the theme: dark by default, the toggle remembered on this screen;
// - the wall clock in Caracas time;
// - the rows sliding in on load;
// - the transcript, timeline and points following their newest entry;
// - ?seguir=1 on the list: opens a new case by itself (the presenter is on
//   stage holding the phone).
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

  function start() {
    wall();
    setInterval(wall, 1000);
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
