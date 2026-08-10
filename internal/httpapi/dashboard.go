package httpapi

// dashboardJS drives the home dashboard: the mobile sidebar, the user menu,
// the add-device panel, the "Agent Śledź" toast, and the live map. It is
// served same-origin (see server.go) because the CSP here has no
// 'unsafe-inline' for scripts.
const dashboardJS = `(function(){
"use strict";
var COLORS = {ok:"#10B981", info:"#0284C7", alert:"#EF4444", offline:"#6B7280"};

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
  if (ageMinutes > 15) return {state:"offline", color:COLORS.offline, label:"Brak sygnału od " + relTime(d.received_at)};
  if (d.speed_kph > 1) return {state:"ok", color:COLORS.ok, label: Math.round(d.speed_kph) + " km/h"};
  return {state:"info", color:COLORS.info, label:"Postój • " + relTime(d.received_at)};
}

// --- Sidebar (mobile) ---
var sidebar = document.getElementById("sidebar");
var backdrop = document.getElementById("sidebar-backdrop");
var menuToggle = document.getElementById("menu-toggle");
function closeSidebar(){ sidebar.classList.remove("open"); backdrop.classList.remove("open"); }
if (menuToggle) menuToggle.addEventListener("click", function(){
  sidebar.classList.toggle("open");
  backdrop.classList.toggle("open");
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

// --- Agent Śledź toast ---
var toast = document.getElementById("agent-toast");
var toastMessage = document.getElementById("toast-message");
var toastClose = document.getElementById("toast-close");
var toastTimer = null;
function showToast(message){
  if (!toast) return;
  toastMessage.textContent = message;
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
  L.tileLayer("https://{s}.basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png", {
    attribution: "&copy; OpenStreetMap contributors &copy; CARTO",
    subdomains: "abcd",
    maxZoom: 20
  }).addTo(map);
  L.control.zoom({position:"bottomright"}).addTo(map);

  var markers = {};
  var didInitialFit = false;

  function markerIcon(cls, moving){
    return L.divIcon({
      className: "",
      html: '<div class="marker' + (moving ? " moving" : "") + '"><div class="marker-pin" style="background:' + cls.color + '"><svg class="icon" viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/></svg></div></div>',
      iconSize: [40, 40],
      iconAnchor: [20, 40],
      popupAnchor: [0, -42]
    });
  }
  function popupHTML(d, cls){
    return '<div class="popup"><div class="popup-head" style="background:' + cls.color + '1a;border-bottom-color:' + cls.color + '40"><strong>' + esc(d.name) + '</strong></div>' +
      '<div class="popup-body">' +
      '<div class="popup-row"><span>Prędkość</span><strong>' + (d.has_position ? Math.round(d.speed_kph) + " km/h" : "—") + '</strong></div>' +
      '<div class="popup-row"><span>Ostatnia aktualizacja</span><span>' + (d.has_position ? relTime(d.received_at) : "—") + '</span></div>' +
      '<div class="popup-row"><span>Status</span><span class="pill ' + cls.state + '">' + esc(cls.label) + '</span></div>' +
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

  function focusDevice(d){
    if (!d.has_position) {
      showToast("Zgubiłem trop! Sprawdź zasilanie trackera lub poczekaj na pierwszy sygnał.");
      return;
    }
    map.flyTo([d.latitude, d.longitude], 15, {duration: 1.3});
    var marker = markers[d.id];
    if (marker) setTimeout(function(){ marker.openPopup(); }, 900);
    showToast('Cel "' + d.name + '" namierzony. Zbliżam widok satelitarny.');
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
        markers[d.id].setIcon(markerIcon(cls, moving));
        markers[d.id].setPopupContent(popupHTML(d, cls));
      } else {
        markers[d.id] = L.marker(latlng, {icon: markerIcon(cls, moving)})
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

  function poll(){
    fetch("/api/positions", {headers: {"Accept": "application/json"}})
      .then(function(r){ return r.ok ? r.json() : []; })
      .then(render)
      .catch(function(){});
  }
  poll();
  setInterval(poll, 5000);

  deviceList.addEventListener("click", function(e){
    var item = e.target.closest(".device-item");
    if (!item) return;
    var id = item.getAttribute("data-id");
    fetch("/api/positions").then(function(r){ return r.json(); }).then(function(devices){
      var d = devices.filter(function(x){ return String(x.id) === id; })[0];
      if (d) focusDevice(d);
    }).catch(function(){});
  });

  setTimeout(function(){
    showToast("Agent Śledź melduje gotowość. Jaki obiekt bierzemy dziś na celownik?");
  }, 1200);
}
})();
`
