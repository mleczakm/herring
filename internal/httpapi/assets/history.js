(function(){
"use strict";
// Route history. Positions come from GET /api/history, or in privacy mode
// from this browser's IndexedDB (HerringPrivacy.route). Both sources return
// the same shape and go through one rendering path: draw().

var MOVING_KPH = 3;          // slower than this counts as standing still
var GAP_MS = 10 * 60000;     // longer silence splits the route and counts as a stop
var STOP_MIN_MS = 3 * 60000; // shorter halts (lights, traffic) are not stops
var DRIFT_M = 50;            // jitter while parked is not distance
var MAX_KPH = 250;           // a faster jump between fixes is a GPS glitch
var MAX_DAYS = 31;           // calendar days; the server allows a little more for DST
var MAX_PAGES = 25;          // 25 × 2000 positions per load at most

// --- Sidebar (mobile) ---
var sidebar = document.getElementById("sidebar");
var backdrop = document.getElementById("sidebar-backdrop");
var menuToggle = document.getElementById("menu-toggle");
function closeSidebar(){ sidebar.classList.remove("open"); backdrop.classList.remove("open"); menuToggle.setAttribute("aria-expanded", "false"); }
if (menuToggle) menuToggle.addEventListener("click", function(){
  sidebar.classList.toggle("open");
  backdrop.classList.toggle("open");
  menuToggle.setAttribute("aria-expanded", String(sidebar.classList.contains("open")));
});
if (backdrop) backdrop.addEventListener("click", closeSidebar);

// --- Collapse: hide the form and keep the summary (phones, where the panel covers the map) ---
var panel = document.getElementById("history-panel");
var collapse = document.getElementById("history-collapse");
if (collapse) collapse.addEventListener("click", function(){
  var collapsed = panel.classList.toggle("collapsed");
  collapse.setAttribute("aria-expanded", String(!collapsed));
  collapse.setAttribute("aria-label", collapsed ? "Rozwiń formularz" : "Zwiń formularz");
});

var form = document.getElementById("history-form");
var mapEl = document.getElementById("map");
if (!mapEl || !window.L) return;

var map = L.map("map", {zoomControl:false}).setView([52.0, 19.3], 6);
L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
  attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
  referrerPolicy: "origin",
  maxZoom: 19
}).addTo(map);
L.control.zoom({position:"bottomright"}).addTo(map);
var routeLayer = L.layerGroup().addTo(map);
if (!form) return;

var deviceInput = document.getElementById("history-device");
var fromInput = document.getElementById("history-from");
var toInput = document.getElementById("history-to");
var statusEl = document.getElementById("history-status");
var statsEl = document.getElementById("history-stats");
var legendEl = document.getElementById("history-legend");
var stopsEl = document.getElementById("history-stops");
var serverRetention = Number(panel.getAttribute("data-retention-days")) || 0;
var privacyLocal = document.body.getAttribute("data-privacy") === "local";

// --- Dates: the inputs hold local calendar days ---
function pad(n){ return (n < 10 ? "0" : "") + n; }
function dayString(d){ return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()); }
function parseDay(s){
  var m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s || "");
  return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])) : null;
}
function addDays(d, n){ return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n); }
function timeLabel(ms, withDay){
  var options = {hour: "2-digit", minute: "2-digit"};
  if (withDay) { options.day = "numeric"; options.month = "short"; }
  return new Date(ms).toLocaleString("pl-PL", options);
}
function dayLabel(ms){ return new Date(ms).toLocaleDateString("pl-PL", {day: "numeric", month: "long", year: "numeric"}); }
function durationLabel(ms){
  var minutes = Math.round(ms / 60000);
  if (minutes < 60) return minutes + " min";
  var hours = Math.floor(minutes / 60);
  return hours + " godz." + (minutes % 60 ? " " + (minutes % 60) + " min" : "");
}
function distanceLabel(m){ return m < 1000 ? Math.round(m) + " m" : (m / 1000).toFixed(m < 10000 ? 1 : 0).replace(".", ",") + " km"; }
function plural(n, one, few, many){
  if (n === 1) return one;
  var t = n % 10, h = n % 100;
  return t >= 2 && t <= 4 && (h < 12 || h > 14) ? few : many;
}

// --- Sources ---
function serverRoute(deviceID, fromMs, toMs){
  var positions = [], oldest = null, retention = serverRetention, pages = 0;
  var base = "/api/history?device=" + encodeURIComponent(deviceID) +
    "&from=" + encodeURIComponent(new Date(fromMs).toISOString().replace(/\.\d+Z$/, "Z")) +
    "&to=" + encodeURIComponent(new Date(toMs).toISOString().replace(/\.\d+Z$/, "Z"));
  function page(cursor){
    return fetch(base + (cursor ? "&cursor=" + encodeURIComponent(cursor) : ""), {headers: {"Accept": "application/json"}, credentials: "same-origin", cache: "no-store"}).then(function(r){
      if (r.status === 409) return {privacyElsewhere: true};
      if (!r.ok) throw new Error("http_" + r.status);
      return r.json().then(function(data){
        positions = positions.concat(data.positions);
        if (data.oldest) oldest = data.oldest;
        retention = data.retention_days;
        if (data.more && ++pages < MAX_PAGES) return page(data.cursor);
        return {positions: positions, oldest: oldest, retentionDays: retention, truncated: !!data.more, place: "server"};
      });
    });
  }
  return page("");
}

// localRoute pulls whatever the server still holds for this browser, then
// reads the route from IndexedDB. Offline it shows what is stored locally.
function localRoute(deviceID, fromMs, toMs){
  if (!window.HerringPrivacy || !window.indexedDB) return Promise.resolve({privacyElsewhere: true});
  return HerringPrivacy.stats().then(function(stats){
    if (!stats.hasKey) return {privacyElsewhere: true};
    return HerringPrivacy.sync().catch(function(){ return {status: "offline"}; }).then(function(sync){
      if (sync.status === "not_bound" || sync.status === "privacy_disabled") return {privacyElsewhere: true};
      return HerringPrivacy.route(deviceID, fromMs, toMs).then(function(result){
        return {positions: result.positions, oldest: result.oldest, retentionDays: stats.retentionDays, truncated: false, place: "browser", offline: sync.status !== "ok"};
      });
    });
  });
}

// --- Route analysis ---
function haversine(a, b){
  var R = 6371000, rad = Math.PI / 180;
  var dLat = (b.lat - a.lat) * rad, dLng = (b.lng - a.lng) * rad;
  var h = Math.sin(dLat / 2) * Math.sin(dLat / 2) + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLng / 2) * Math.sin(dLng / 2);
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
}

// prepare keeps valid fixes, sorted by tracker time, without duplicates.
function prepare(raw){
  var points = [];
  raw.forEach(function(p){
    if (p.gps_valid === false) return;
    var t = Date.parse(p.tracker_time);
    if (!isFinite(t)) return;
    points.push({t: t, lat: p.latitude, lng: p.longitude, speed: p.speed_kph || 0, sms: p.source === "sms"});
  });
  points.sort(function(a, b){ return a.t - b.t; });
  return points.filter(function(p, i){
    var q = points[i - 1];
    return !q || q.t !== p.t || q.lat !== p.lat || q.lng !== p.lng;
  });
}

function analyse(points){
  var result = {distance: 0, driving: 0, maxSpeed: 0, stops: [], segments: [], gaps: []};
  // A stop runs from the last fix in motion (a parked tracker reports only
  // every few minutes) to the next one in motion, and sits where the car stood.
  var segment = [], stopStart = -1, stopAt = -1, seenMoving = false;
  function moving(p){ return !p.sms && p.speed > MOVING_KPH; }
  function closeStop(b){
    var a = stopStart, at = points[stopAt];
    if (seenMoving && points[b].t - points[a].t >= STOP_MIN_MS) result.stops.push({lat: at.lat, lng: at.lng, from: points[a].t, to: points[b].t});
  }
  points.forEach(function(p, i){
    if (!p.sms && p.speed > result.maxSpeed) result.maxSpeed = p.speed;
    var prev = points[i - 1];
    if (prev) {
      var dt = p.t - prev.t, d = haversine(prev, p);
      var gap = dt > GAP_MS;
      if (gap) {
        if (segment.length) result.segments.push(segment);
        segment = [];
        result.gaps.push([[prev.lat, prev.lng], [p.lat, p.lng]]);
        if (stopStart < 0) stopStart = stopAt = i - 1;
      } else if (moving(prev) || moving(p)) {
        result.driving += dt;
      }
      var glitch = dt > 0 && d / (dt / 3600000) / 1000 > MAX_KPH;
      var drift = !moving(prev) && !moving(p) && d < DRIFT_M;
      if (!glitch && !drift) result.distance += d;
    }
    segment.push([p.lat, p.lng]);
    if (moving(p)) {
      if (stopStart >= 0) closeStop(i);
      stopStart = -1;
      seenMoving = true;
    } else if (stopStart < 0) {
      stopStart = i > 0 ? i - 1 : i;
      stopAt = i;
    }
  });
  if (segment.length) result.segments.push(segment);
  return result;
}

// --- Rendering (shared by both sources) ---
function flag(kind, label){
  return L.divIcon({className: "", html: '<span class="route-flag ' + kind + '"><span>' + label + '</span></span>', iconSize: [16, 16], iconAnchor: [8, 8], popupAnchor: [0, -24]});
}
function fitRoute(bounds){
  if (!bounds.isValid()) return;
  var rect = panel.getBoundingClientRect(), mapRect = mapEl.getBoundingClientRect();
  var wide = window.matchMedia("(min-width: 769px)").matches;
  var options = {maxZoom: 16, animate: false};
  if (wide) { options.paddingTopLeft = [rect.right - mapRect.left + 24, 80]; options.paddingBottomRight = [40, 40]; }
  else { options.paddingTopLeft = [24, rect.bottom - mapRect.top + 24]; options.paddingBottomRight = [24, 40]; }
  map.fitBounds(bounds, options);
}
function setStatus(html){ statusEl.innerHTML = html; statusEl.hidden = !html; }
function clearRoute(){
  routeLayer.clearLayers();
  statsEl.hidden = true; legendEl.hidden = true; stopsEl.hidden = true; stopsEl.innerHTML = "";
}

// retentionNote explains missing data when the chosen period reaches past
// the retention window, so older positions have been deleted.
function retentionNote(result, fromMs){
  if (!result.retentionDays || fromMs >= Date.now() - result.retentionDays * 86400000) return "";
  var days = result.retentionDays + " " + plural(result.retentionDays, "dzień", "dni", "dni");
  return (result.place === "browser" ? "Ta przeglądarka przechowuje pozycje przez " : "Serwer przechowuje pozycje przez ") + days + ", starsze usuwa.";
}

function draw(result, fromMs, toMs){
  clearRoute();
  var points = prepare(result.positions);
  var notes = [];
  if (result.offline) notes.push("Brak połączenia z serwerem. Pokazuję pozycje zapisane na tym urządzeniu.");
  if (!points.length) {
    var oldestMs = result.oldest ? Date.parse(result.oldest) : NaN;
    var lines = ["<strong>Brak pozycji w tym okresie</strong>"];
    if (!result.oldest) lines.push("Ten lokalizator nie ma jeszcze zapisanych pozycji" + (result.place === "browser" ? " na tym urządzeniu." : "."));
    else if (oldestMs >= toMs) lines.push("Najstarsza zapisana pozycja jest z " + dayLabel(oldestMs) + ".");
    else lines.push("Lokalizator nie wysłał wtedy żadnej pozycji z GPS.");
    var note = retentionNote(result, fromMs);
    if (note) lines.push(note);
    setStatus(notes.concat(lines).map(function(l){ return "<span>" + l + "</span>"; }).join(""));
    return;
  }

  var route = analyse(points);
  var multiDay = toMs - fromMs > 86400000;
  var first = points[0], last = points[points.length - 1];

  route.gaps.forEach(function(g){ L.polyline(g, {color: "#0e3332", weight: 2, opacity: .55, dashArray: "4 8", interactive: false}).addTo(routeLayer); });
  route.segments.forEach(function(s){
    if (s.length > 1) L.polyline(s, {color: "#f05c2e", weight: 4, opacity: .9, lineJoin: "round"}).addTo(routeLayer);
  });
  route.stops.forEach(function(s, i){
    s.marker = L.circleMarker([s.lat, s.lng], {radius: 7, color: "#fff", weight: 2, fillColor: "#0e3332", fillOpacity: 1})
      .bindPopup('<div class="popup"><div class="popup-body"><strong>Postój ' + (i + 1) + '</strong><span>' + timeLabel(s.from, multiDay) + ' – ' + timeLabel(s.to, multiDay) + ' · ' + durationLabel(s.to - s.from) + '</span></div></div>', {closeButton: false})
      .addTo(routeLayer);
  });
  var smsCount = 0;
  points.forEach(function(p){
    if (!p.sms) return;
    smsCount++;
    L.circleMarker([p.lat, p.lng], {radius: 6, color: "#c99a4a", weight: 3, fillColor: "#fffaf0", fillOpacity: 1})
      .bindPopup('<div class="popup"><div class="popup-body"><strong>Pozycja z SMS-a</strong><span>' + timeLabel(p.t, multiDay) + '</span></div></div>', {closeButton: false})
      .addTo(routeLayer);
  });
  var startPopup = '<div class="popup"><div class="popup-body"><strong>Start</strong><span>' + timeLabel(first.t, multiDay) + '</span></div></div>';
  var endPopup = '<div class="popup"><div class="popup-body"><strong>Koniec</strong><span>' + timeLabel(last.t, multiDay) + '</span></div></div>';
  if (points.length > 1) L.marker([first.lat, first.lng], {icon: flag("start", "Start"), zIndexOffset: 500}).bindPopup(startPopup, {closeButton: false}).addTo(routeLayer);
  L.marker([last.lat, last.lng], {icon: flag("end", points.length > 1 ? "Koniec" : "Pozycja"), zIndexOffset: 600}).bindPopup(endPopup, {closeButton: false}).addTo(routeLayer);

  document.getElementById("stat-distance").textContent = distanceLabel(route.distance);
  document.getElementById("stat-driving").textContent = durationLabel(route.driving);
  document.getElementById("stat-stops").textContent = String(route.stops.length);
  document.getElementById("stat-speed").textContent = Math.round(route.maxSpeed) + " km/h";
  statsEl.hidden = false;
  legendEl.hidden = !(smsCount || route.stops.length);
  legendEl.querySelector(".legend-item.sms").hidden = !smsCount;
  legendEl.querySelector(".legend-item.stop").hidden = !route.stops.length;

  if (route.stops.length) {
    route.stops.forEach(function(s, i){
      var li = document.createElement("li");
      var button = document.createElement("button");
      button.type = "button";
      button.innerHTML = '<span class="stop-index">' + (i + 1) + '</span><span>' + timeLabel(s.from, multiDay) + ' – ' + timeLabel(s.to, multiDay) + '</span><span class="stop-duration">' + durationLabel(s.to - s.from) + '</span>';
      button.addEventListener("click", function(){
        map.flyTo([s.lat, s.lng], Math.max(map.getZoom(), 16), {duration: .8});
        setTimeout(function(){ s.marker.openPopup(); }, 850);
      });
      li.appendChild(button);
      stopsEl.appendChild(li);
    });
    stopsEl.hidden = false;
  }

  var summary = points.length + " " + plural(points.length, "pozycja", "pozycje", "pozycji") + " · " + timeLabel(first.t, multiDay) + " – " + timeLabel(last.t, multiDay);
  if (result.truncated) notes.push("To tylko początek trasy. Wybierz krótszy okres, żeby zobaczyć całość.");
  var note = result.oldest && Date.parse(result.oldest) > fromMs ? retentionNote(result, fromMs) : "";
  if (note) notes.push("Początek tego okresu jest już usunięty. " + note);
  setStatus(notes.concat([summary]).map(function(l){ return "<span>" + l + "</span>"; }).join(""));
  fitRoute(L.latLngBounds(points.map(function(p){ return [p.lat, p.lng]; })));
}

// --- Loading ---
var loadSeq = 0;
function load(){
  var from = parseDay(fromInput.value), to = parseDay(toInput.value);
  if (!from || !to) return;
  if (to < from) { setStatus("<span>Data „Do” musi być taka sama jak „Od” albo późniejsza.</span>"); clearRoute(); return; }
  var toEnd = addDays(to, 1);
  if ((toEnd - from) / 86400000 > MAX_DAYS + 0.1) { setStatus("<span>Wybierz najwyżej " + MAX_DAYS + " dni naraz.</span>"); clearRoute(); return; }
  var fromMs = from.getTime(), toMs = toEnd.getTime(), deviceID = deviceInput.value;
  try {
    history.replaceState(null, "", "/historia?urzadzenie=" + encodeURIComponent(deviceID) + "&od=" + fromInput.value + "&do=" + toInput.value);
  } catch (e) {}
  var seq = ++loadSeq;
  setStatus("<span>Śledź przegląda zapiski…</span>");
  panel.setAttribute("aria-busy", "true");
  (privacyLocal ? localRoute(deviceID, fromMs, toMs) : serverRoute(deviceID, fromMs, toMs)).then(function(result){
    if (seq !== loadSeq) return;
    if (result.privacyElsewhere) {
      clearRoute();
      setStatus("<strong>Historia jest na innym urządzeniu</strong><span>Tryb prywatny jest włączony. Trasy są zapisane tylko w aplikacji na urządzeniu, na którym go włączono. Serwer trzyma pozycje tylko do chwili, gdy ta aplikacja je pobierze.</span>");
      return;
    }
    draw(result, fromMs, toMs);
  }, function(){
    if (seq !== loadSeq) return;
    clearRoute();
    setStatus("<span>Nie udało się wczytać trasy. Spróbuj ponownie za chwilę.</span>");
  }).then(function(){ if (seq === loadSeq) panel.removeAttribute("aria-busy"); });
}

// --- Initial state from the URL, defaulting to today ---
var params = new URLSearchParams(location.search);
var today = new Date();
var wantDevice = params.get("urzadzenie");
var tracked = deviceInput.querySelector("option[data-tracked]");
if (wantDevice && deviceInput.querySelector('option[value="' + CSS.escape(wantDevice) + '"]')) deviceInput.value = wantDevice;
else if (tracked) deviceInput.value = tracked.value;
fromInput.value = parseDay(params.get("od")) ? params.get("od") : dayString(today);
toInput.value = parseDay(params.get("do")) ? params.get("do") : fromInput.value;
fromInput.max = toInput.max = dayString(today);

form.addEventListener("submit", function(e){ e.preventDefault(); load(); });
deviceInput.addEventListener("change", load);
fromInput.addEventListener("change", function(){
  if (parseDay(fromInput.value) && (!parseDay(toInput.value) || toInput.value < fromInput.value)) toInput.value = fromInput.value;
  load();
});
toInput.addEventListener("change", load);
form.querySelectorAll("[data-days]").forEach(function(button){
  button.addEventListener("click", function(){
    var days = Number(button.getAttribute("data-days"));
    var now = new Date();
    if (days === 7) { fromInput.value = dayString(addDays(now, -6)); toInput.value = dayString(now); }
    else { fromInput.value = toInput.value = dayString(addDays(now, -days)); }
    load();
  });
});
load();
})();
