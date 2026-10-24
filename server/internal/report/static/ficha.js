// The incident report page (#76): tapping a transcript turn (or the quoted
// phrase in the timeline) plays the call's recording from that second, and
// the turn that is playing is highlighted. No dependencies, no requests
// beyond the recording itself.
(function () {
  "use strict";

  function audioOf(call) {
    return document.getElementById("audio-" + call);
  }

  function seek(audio, secs) {
    var go = function () {
      try { audio.currentTime = secs; } catch (e) { /* not seekable yet */ }
      var p = audio.play();
      if (p && p.catch) { p.catch(function () {}); }
    };
    if (audio.readyState >= 1) {
      go();
    } else {
      audio.addEventListener("loadedmetadata", go, { once: true });
      audio.load();
    }
  }

  document.addEventListener("click", function (ev) {
    var el = ev.target.closest("[data-seek]");
    if (!el) { return; }
    var audio = audioOf(el.getAttribute("data-call"));
    if (!audio) { return; }
    ev.preventDefault();
    if (el.hasAttribute("data-scroll")) {
      var card = document.getElementById("llamada-" + el.getAttribute("data-call"));
      if (card) { card.scrollIntoView({ behavior: "smooth", block: "start" }); }
    }
    seek(audio, parseFloat(el.getAttribute("data-seek")) || 0);
  });

  // Highlight the turn whose start is the last one at or before the
  // playback position.
  Array.prototype.forEach.call(document.querySelectorAll("audio[data-call]"), function (audio) {
    var call = audio.getAttribute("data-call");
    var turns = Array.prototype.slice.call(document.querySelectorAll('.turn[data-call="' + call + '"]'));
    var current = null;
    function mark() {
      var t = audio.currentTime, best = null;
      turns.forEach(function (el) {
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
      if (note) { note.hidden = false; }
      audio.hidden = true;
    });
  });
})();
