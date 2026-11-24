package httpapi

// dashboardJS drives the home dashboard: the mobile sidebar, the user menu,
// the add-device panel, the "Agent Śledź" toast, and the live map. It is
// served same-origin (see server.go) because the CSP here has no
// 'unsafe-inline' for scripts.
const dashboardJS = `(function(){
"use strict";
var COLORS = {ok:"#267459", info:"#32626a", alert:"#b94d36", offline:"#586c6c"};

function esc(s){var d=document.createElement("div");d.textContent=s==null?"":s;return d.innerHTML;}
function relTime(iso){
  var seconds = Math.max(0, (Date.now() - new Date(iso).getTime())/1000);
  if (seconds < 60) return "przed chwilą";
  var minutes = Math.round(seconds/60);
  if (minutes < 60) return "" + minutes + " min temu";
  var hours = Math.round(minutes/60);
  if (hours < 24) return "" + hours + " godz. temu";
  return "" + Math.round(hours/24) + " dni temu";
}
function classify(d){
  if (d.config_status !== "configured") {
    return {state:"offline", color:COLORS.offline, label: d.config_status === "failed" ? "⚠ Konfiguracja nieudana" : "⏳ Konfigurowanie…"};
  }
  if (!d.has_position) {
    return {state:"offline", color:COLORS.offline, label:"Oczekiwanie na pierwszy sygnał"};
  }
  var ageMinutes = (Date.now() - new Date(d.received_at).getTime())/60000;
  if (!d.gps_valid) return {state:"alert", color:COLORS.alert, label:"Brak fixu GPS"};
  if (d.source === "sms") return {state:"info", color:COLORS.info, label:"Pozycja z SMS-a • " + relTime(d.received_at)};
  if (ageMinutes > 15) return {state:"offline", color:COLORS.offline, label:"Brak sygnału od " + relTime(d.received_at)};
  if (d.speed_kph > 1) return {state:"ok", color:COLORS.ok, label: Math.round(d.speed_kph || 0) + " km/h"};
  return {state:"info", color:COLORS.info, label:"Postój • " + relTime(d.received_at)};
}

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

// --- User menu ---
var userToggle = document.getElementById("user-menu-toggle");
var userDropdown = document.getElementById("user-dropdown");
if (userToggle) userToggle.addEventListener("click", function(e){
  e.stopPropagation();
  userDropdown.classList.toggle("open");
});
document.addEventListener("click", function(){ userDropdown && userDropdown.classList.remove("open"); });

// --- Add-device panel ---
var addPanel = document.getElementById("add-device-panel");
var openAdd = document.getElementById("open-add-device");
var closeAdd = document.getElementById("close-add-device");
if (openAdd) openAdd.addEventListener("click", function(){ addPanel.classList.add("open"); });
if (closeAdd) closeAdd.addEventListener("click", function(){ addPanel.classList.remove("open"); });
// /#dodaj (linked from the devices page) opens the form straight away.
if (addPanel && window.location.hash === "#dodaj") addPanel.classList.add("open");
// Picking a recently rejected tracker fills in its ID; the admin still submits.
var trackerInput = document.getElementById("add-tracker-id");
if (addPanel && trackerInput) addPanel.addEventListener("click", function(e){
  var chip = e.target.closest && e.target.closest("[data-tracker-id]");
  if (!chip) return;
  trackerInput.value = chip.getAttribute("data-tracker-id");
  trackerInput.focus();
});

// --- Agent Śledź toast ---
var toast = document.getElementById("agent-toast");
var toastMessage = document.getElementById("toast-message");
var toastClose = document.getElementById("toast-close");
var toastTimer = null;
var toastAction = document.getElementById("toast-action");
var toastActionHandler = null;
if (toastAction) toastAction.addEventListener("click", function(){ if (toastActionHandler) toastActionHandler(); });
// showToast shows a message with an optional {label, run} action button.
function showToast(message, action){
  if (!toast) return;
  toastMessage.textContent = message;
  if (toastAction) {
    toastAction.hidden = !action;
    toastAction.textContent = action ? action.label : "";
    toastActionHandler = action ? action.run : null;
  }
  toast.classList.add("show");
  if (toastTimer) clearTimeout(toastTimer);
  toastTimer = setTimeout(function(){ toast.classList.remove("show"); }, 6000);
}
if (toastClose) toastClose.addEventListener("click", function(){ toast.classList.remove("show"); });

// --- Device search filter ---
var search = document.getElementById("device-search");
var deviceList = document.getElementById("device-list");
if (search) search.addEventListener("input", function(){
  var q = search.value.trim().toLowerCase();
  var items = deviceList.querySelectorAll(".device-item");
  for (var i = 0; i < items.length; i++) {
    items[i].style.display = items[i].textContent.toLowerCase().indexOf(q) === -1 ? "none" : "";
  }
});

// --- Map ---
var mapEl = document.getElementById("map");
if (mapEl && window.L) {
  var map = L.map("map", {zoomControl:false}).setView([52.0, 19.3], 6);
  L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
    referrerPolicy: "origin",
    maxZoom: 19
  }).addTo(map);
  L.control.zoom({position:"bottomright"}).addTo(map);

  var markers = {};
  var didInitialFit = false;

  function markerIcon(cls, moving, d){
    return L.divIcon({
      className: "",
      html: '<div class="marker' + (moving ? " moving" : "") + '"><div class="marker-pin" style="background:' + cls.color + '"><span style="font-size:18px;color:white;transform:rotate(45deg)">' + window.vehicleSymbol(d.vehicle_type,d.map_icon) + '</span></div></div>',
      iconSize: [40, 40],
      iconAnchor: [20, 40],
      popupAnchor: [0, -42]
    });
  }
  function popupHTML(d, cls){
    return '<div class="popup"><div class="popup-head" style="background:' + cls.color + '1a;border-bottom-color:' + cls.color + '40"><strong>' + esc(d.name) + '</strong></div>' +
      '<div class="popup-body">' +
      '<div class="popup-row"><span>Prędkość</span><strong>' + (d.has_position ? Math.round(d.speed_kph || 0) + " km/h" : "—") + '</strong></div>' +
      '<div class="popup-row"><span>Ostatnia aktualizacja</span><span>' + (d.has_position ? relTime(d.received_at) : "—") + '</span></div>' +
      '<div class="popup-row"><span>Status</span><span class="pill ' + cls.state + '">' + esc(cls.label) + '</span></div>' +
      '<p><a href="/pojazdy/' + encodeURIComponent(d.id) + '">Ustawienia pojazdu</a> · <a href="/trasy?id=' + encodeURIComponent(d.id) + '">Przejazdy</a></p>' +
      '<button class="btn btn-small popup-locate" type="button" data-locate="' + esc(String(d.id)) + '">Zapytaj SMS-em o pozycję</button>' +
      '</div></div>';
  }

  function updateDeviceItem(item, d, cls){
    var avatar = item.querySelector(".device-avatar");
    var summary = item.querySelector("[data-role=summary]");
    if (avatar) avatar.style.background = cls.color;
    if (summary) {
      summary.innerHTML = "";
      var pill = document.createElement("span");
      pill.className = "pill " + cls.state;
      pill.textContent = cls.label;
      summary.appendChild(pill);
    }
  }

  // requestSMSLocation asks the tracker to answer with its position by SMS,
  // which works when it has no mobile data but still has GSM coverage.
  function requestSMSLocation(id){
    fetch("/api/devices/" + encodeURIComponent(id) + "/locate", {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}", credentials: "same-origin"})
      .then(function(r){
        if (r.status === 202) showToast("Wysłałem SMS do lokalizatora. Odpowiedź z pozycją zwykle przychodzi w ciągu minuty.");
        else if (r.status === 429) showToast("Zapytanie poszło przed chwilą. Poczekaj na odpowiedź, zanim wyślesz kolejne.");
        else if (r.status === 503) showToast("Wysyłanie SMS-ów nie jest skonfigurowane na serwerze.");
        else showToast("Nie udało się wysłać SMS-a. Spróbuj ponownie za chwilę.");
      })
      .catch(function(){ showToast("Brak połączenia z serwerem."); });
  }
  mapEl.addEventListener("click", function(e){
    var button = e.target.closest && e.target.closest("[data-locate]");
    if (button) requestSMSLocation(button.getAttribute("data-locate"));
  });

  function focusDevice(d){
    if (!d.has_position) {
      showToast("Brak pozycji. Lokalizator może nie mieć internetu albo jeszcze się nie odezwał.", {label: "Zapytaj SMS-em", run: function(){ requestSMSLocation(d.id); }});
      return;
    }
    map.flyTo([d.latitude, d.longitude], 15, {duration: 1.3});
    var marker = markers[d.id];
    if (marker) setTimeout(function(){ marker.openPopup(); }, 900);
    showToast('Urządzenie "' + d.name + '" na mapie.');
  }

  function render(devices){
    var bounds = [];
    devices.forEach(function(d){
      var cls = classify(d);
      var item = deviceList.querySelector('.device-item[data-id="' + d.id + '"]');
      if (item) updateDeviceItem(item, d, cls);

      if (!d.has_position) {
        if (markers[d.id]) { map.removeLayer(markers[d.id]); delete markers[d.id]; }
        return;
      }
      var latlng = [d.latitude, d.longitude];
      bounds.push(latlng);
      var moving = cls.state === "ok";
      if (markers[d.id]) {
        markers[d.id].setLatLng(latlng);
        markers[d.id].setIcon(markerIcon(cls, moving, d));
        markers[d.id].setPopupContent(popupHTML(d, cls));
      } else {
        markers[d.id] = L.marker(latlng, {icon: markerIcon(cls, moving, d)})
          .addTo(map)
          .bindPopup(popupHTML(d, cls), {maxWidth: 300, minWidth: 220, closeButton: false});
      }
    });
    if (!didInitialFit && bounds.length) {
      didInitialFit = true;
      if (bounds.length === 1) map.setView(bounds[0], 14);
      else map.fitBounds(bounds, {padding: [40, 40]});
    }
  }

  var lastDevices = [];
  function show(devices){ lastDevices = devices; render(devices); }
  function serverPoll(){
    return fetch("/api/positions", {headers: {"Accept": "application/json"}})
      .then(function(r){ return r.ok ? r.json() : []; })
      .then(show);
  }

  // Privacy mode: the server hands positions only to the bound browser, which
  // stores them in IndexedDB. The map is drawn from that local copy, so it
  // also works offline. Other browsers see the trackers without positions.
  var privacyLocal = document.body.getAttribute("data-privacy") === "local" && window.HerringPrivacy && window.indexedDB;
  var privacyNotice = false;
  function notifyOnce(message){ if (!privacyNotice) { privacyNotice = true; showToast(message); } }
  function privacyPoll(){
    return HerringPrivacy.sync().then(function(result){
      if (result.status === "no_key" || result.status === "not_bound" || result.status === "privacy_disabled") {
        notifyOnce("Tryb prywatny: historia pozycji jest dostępna tylko na urządzeniu, na którym go włączono.");
        return serverPoll();
      }
      if (result.status !== "ok") notifyOnce("Brak połączenia z serwerem. Pokazuję pozycje zapisane na tym urządzeniu.");
      return HerringPrivacy.devices().then(show);
    }, function(){
      notifyOnce("Brak połączenia z serwerem. Pokazuję pozycje zapisane na tym urządzeniu.");
      return HerringPrivacy.devices().then(show);
    });
  }

  var polling = false;
  function poll(){
    if (polling) return;
    polling = true;
    (privacyLocal ? privacyPoll() : serverPoll())
      .catch(function(){})
      .then(function(){ polling = false; });
  }
  poll();
  setInterval(poll, 5000);

  deviceList.addEventListener("click", function(e){
    var item = e.target.closest(".device-item");
    if (!item) return;
    var id = item.getAttribute("data-id");
    var d = lastDevices.filter(function(x){ return String(x.id) === id; })[0];
    if (d) focusDevice(d);
  });
}
})();
`
