// The route on a real map (#76), laid over the route the server draws:
// Leaflet (leaflet.js, vendored) with CARTO's basemap tiles. The tiles are
// the only request the page makes outside this server (the CSP allows them
// as images, MapTileHosts in page.go), and the page sends no Referer with
// them, so the tile server never sees the report's link. CARTO's tiles need
// a key (CARTO_BASEMAPS_KEY, on <body data-map-key>); without one they come
// back stamped "API KEY REQUIRED", so with no key there is no map at all.
//
// - The drawing stays underneath: it is what shows with no JS, until the
//   first tile arrives, and for good when the tiles cannot load.
// - The points come in the route box's data-points (JSON). A live update
//   rewrites that attribute in place and keeps the map div, whose HTML never
//   changes, so the map survives every update; ficha.js says when one
//   happened with a "ficha:update" event.
// - The view follows the route until the viewer drags or zooms it.
// - Wheel zoom, and one-finger drag on a touch screen, wait for a tap on the
//   map, so scrolling the page past it still scrolls the page.
(function () {
  "use strict";

  var L = window.L;
  var key = document.body && document.body.getAttribute("data-map-key");
  if (!L || !key || !document.querySelector) { return; }

  var MAX_FIT_ZOOM = 17; // a single point is shown with its block, not its sidewalk
  var ATTRIBUTION = '&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener noreferrer">OpenStreetMap</a> contributors ' +
    '&copy; <a href="https://carto.com/attributions" target="_blank" rel="noopener noreferrer">CARTO</a>';
  var motion = !(window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches);
  var coarse = !!(window.matchMedia && window.matchMedia("(pointer: coarse)").matches);
  var maps = [];

  // ------------------------------------------------------------ theme

  // The central sets <html data-theme>; the family page follows the system.
  function dark() {
    var t = document.documentElement.getAttribute("data-theme");
    if (t) { return t === "dark"; }
    return !!(window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches);
  }
  function tileURL() {
    return "https://{s}.basemaps.cartocdn.com/rastertiles/" + (dark() ? "dark_all" : "light_all") + "/{z}/{x}/{y}{r}.png?key=" + encodeURIComponent(key);
  }
  function retheme() {
    var url = tileURL();
    maps.forEach(function (st) { if (st.url !== url) { st.url = url; st.tiles.setUrl(url); } });
  }

  // ------------------------------------------------------------ the map

  // text is a tooltip's content as text, never as HTML.
  function text(s) {
    var span = document.createElement("span");
    span.textContent = s;
    return span;
  }

  function mount(el) {
    var map = L.map(el, {
      scrollWheelZoom: false,
      dragging: !coarse,
      zoomAnimation: motion,
      fadeAnimation: motion,
      markerZoomAnimation: motion,
      maxZoom: 19
    });
    el.tabIndex = -1; // out of the tab order while the drawing still shows
    map.attributionControl.setPrefix(false);
    map.createPane("acc").style.zIndex = 390; // the accuracy circle, under the route (overlayPane is 400)
    var url = tileURL();
    var tiles = L.tileLayer(url, { subdomains: "abcd", maxZoom: 19, attribution: ATTRIBUTION }).addTo(map);
    var st = {
      el: el, box: el.parentNode, map: map, tiles: tiles, url: url,
      line: L.polyline([], { className: "map-line", interactive: false }).addTo(map),
      dots: L.layerGroup().addTo(map),
      acc: null, me: null, raw: null, pts: null, user: false, fitting: false
    };
    // The first tile shows the map; until then (and when none ever comes)
    // the drawing is what the reader sees.
    tiles.once("tileload", function () {
      el.classList.add("on");
      el.removeAttribute("aria-hidden");
      el.setAttribute("role", "region");
      el.setAttribute("aria-label", "Mapa del recorrido");
      el.tabIndex = 0;
    });
    // A move the page did not start is the viewer's: from then on the view
    // stays where they left it.
    map.on("movestart", function () { if (!st.fitting) { st.user = true; } });
    map.on("moveend", function () { st.fitting = false; });
    arm(st);
    if (window.ResizeObserver) {
      st.ro = new ResizeObserver(function () { map.invalidateSize({ pan: false }); if (st.pts) { fit(st, false); } });
      st.ro.observe(el);
    }
    el._route = st;
    maps.push(st);
    return st;
  }

  // arm turns on wheel zoom (and one-finger drag on a touch screen) when the
  // viewer taps or focuses the map, and off when they leave it.
  function arm(st) {
    var on = false;
    function set(v) {
      if (on === v) { return; }
      on = v;
      if (v) { st.map.scrollWheelZoom.enable(); } else { st.map.scrollWheelZoom.disable(); }
      if (coarse) { if (v) { st.map.dragging.enable(); } else { st.map.dragging.disable(); } }
    }
    st.el.addEventListener("click", function () { set(true); });
    st.el.addEventListener("focus", function () { set(true); });
    st.el.addEventListener("mouseleave", function () { set(false); });
    document.addEventListener("click", function (e) { if (!st.el.contains(e.target)) { set(false); } }, true);
  }

  // update draws the points in the route box's data-points, if they changed.
  function update(st) {
    var raw = st.box.getAttribute("data-points");
    if (raw === st.raw) { return; }
    var pts;
    try { pts = JSON.parse(raw || "[]"); } catch (e) { return; }
    if (!pts.length) { return; }
    var first = st.raw === null;
    st.raw = raw;
    st.pts = pts;
    draw(st, pts);
    fit(st, !first);
  }

  function draw(st, pts) {
    var lls = pts.map(function (p) { return L.latLng(p.lat, p.lng); });
    var n = pts.length - 1;
    st.line.setLatLngs(lls);
    st.dots.clearLayers();
    pts.forEach(function (p, i) {
      if (i < n) {
        st.dots.addLayer(L.circleMarker(lls[i], { radius: i === 0 ? 7 : 6, className: "map-dot" + (i === 0 ? " first" : "") })
          .bindTooltip(text(p.title), { direction: "top", offset: [0, -6] }));
      }
      if (p.label) { // the central's times, as on its drawing
        st.dots.addLayer(L.tooltip({ permanent: true, interactive: false, direction: "right", offset: [i === n ? 20 : 9, 0], className: "map-time" + (i === n ? " newest" : "") })
          .setLatLng(lls[i]).setContent(text(p.label)));
      }
    });
    var last = pts[n];
    if (last.acc) {
      if (st.acc) { st.acc.setLatLng(lls[n]).setRadius(last.acc); } else {
        st.acc = L.circle(lls[n], { radius: last.acc, pane: "acc", className: "map-acc", interactive: false }).addTo(st.map);
      }
    } else if (st.acc) {
      st.acc.remove();
      st.acc = null;
    }
    if (st.me) { st.me.setLatLng(lls[n]).setTooltipContent(text(last.title)); } else {
      st.me = L.marker(lls[n], {
        icon: L.divIcon({ className: "map-newest", html: '<span class="map-pulse"></span><span class="map-me"></span>', iconSize: [28, 28] }),
        keyboard: false, zIndexOffset: 1000
      }).bindTooltip(text(last.title), { direction: "top", offset: [0, -10] }).addTo(st.map);
    }
  }

  // fit shows the whole route and the newest point's accuracy circle, unless
  // the viewer moved the map.
  function fit(st, animate) {
    if (st.user || !st.pts) { return; }
    var pts = st.pts, last = pts[pts.length - 1];
    var b = L.latLngBounds(pts.map(function (p) { return [p.lat, p.lng]; }));
    if (last.acc) { b.extend(L.latLng(last.lat, last.lng).toBounds(last.acc * 2)); }
    st.fitting = true;
    st.map.fitBounds(b, { padding: [28, 28], maxZoom: MAX_FIT_ZOOM, animate: animate && motion });
  }

  // ------------------------------------------------------------ start

  // scan mounts a map in every route box that has none yet and brings each
  // one up to date.
  function scan() {
    maps = maps.filter(function (st) {
      if (document.body.contains(st.el)) { return true; }
      if (st.ro) { st.ro.disconnect(); }
      st.map.remove();
      return false;
    });
    Array.prototype.forEach.call(document.querySelectorAll("[data-route-map]"), function (el) {
      try { update(el._route || mount(el)); } catch (e) { if (window.console) { console.error(e); } } // the drawing stays
    });
  }

  function start() {
    scan();
    document.addEventListener("ficha:update", scan);
    new MutationObserver(retheme).observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    var mq = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)");
    if (mq && mq.addEventListener) { mq.addEventListener("change", retheme); }
  }
  if (document.readyState === "loading") { document.addEventListener("DOMContentLoaded", start); } else { start(); }
})();
