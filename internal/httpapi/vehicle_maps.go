package httpapi

const vehicleMapsJS = `(function(){
"use strict";
const symbols = Object.freeze({car:"🚗",bike:"🚲",motorcycle:"🏍️",scooter:"🛴",van:"🚐",truck:"🚚",trailer:"🛞",boat:"⛵",pin:"📍",dot:"●",other:"●"});
window.vehicleSymbol = function(kind, icon) {
  const key = !icon || icon === "auto" ? kind : icon;
  return Object.hasOwn(symbols, key) ? symbols[key] : symbols.other;
};
const toggle = document.getElementById("menu-toggle");
const sidebar = document.getElementById("sidebar");
const backdrop = document.getElementById("sidebar-backdrop");
if (toggle && sidebar && backdrop && !document.getElementById("map")) {
  function open(on) {
    sidebar.classList.toggle("open",on);
    backdrop.classList.toggle("open",on);
    toggle.setAttribute("aria-expanded",String(on));
  }
  toggle.addEventListener("click",() => open(!sidebar.classList.contains("open")));
  backdrop.addEventListener("click",() => open(false));
}

const shareLink = document.getElementById("share-link");
if (shareLink) {
  shareLink.value = new URL(shareLink.value,window.location.origin).href;
  const button = document.getElementById("copy-share");
  button.addEventListener("click",async () => {
    try {
      await navigator.clipboard.writeText(shareLink.value);
      button.textContent = "Skopiowano";
    } catch (_) {
      shareLink.select();
    }
  });
}

function showMap(element) {
  element.hidden = false;
  const map = L.map(element);
  L.tileLayer("https://tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a>',
    referrerPolicy:"origin", maxZoom:19
  }).addTo(map);
  return map;
}
document.querySelectorAll("[data-track]").forEach(button => {
  const section = button.closest("section");
  const element = section.querySelector("[data-track-map]");
  const status = section.querySelector("[data-track-status]");
  let map;
  button.addEventListener("click",async () => {
    if (map) {
      element.hidden = !element.hidden;
      button.textContent = element.hidden ? "Pokaż trasę" : "Ukryj trasę";
      if (!element.hidden) map.invalidateSize();
      return;
    }
    button.disabled = true;
    status.hidden = true;
    try {
      const response = await fetch(button.dataset.track);
      if (!response.ok) throw new Error();
      const points = await response.json();
      element.style.height = "320px";
      map = showMap(element);
      const line = L.polyline(points.map(p => [p.lat,p.lon])).addTo(map);
      map.fitBounds(line.getBounds(),{padding:[16,16],maxZoom:17});
      button.textContent = "Ukryj trasę";
    } catch (_) {
      status.hidden = false;
      status.textContent = "Nie udało się pobrać trasy.";
    } finally {
      button.disabled = false;
    }
  });
});

const shared = document.getElementById("shared-map");
if (shared) {
  const status = document.getElementById("share-status");
  let map, marker;
  async function refresh() {
    try {
      const response = await fetch("/api" + window.location.pathname,{cache:"no-store"});
      if (response.status === 404) {
        if (map) map.remove();
        shared.hidden = true;
        status.textContent = "Link wygasł lub został odwołany.";
        return;
      }
      if (!response.ok) throw new Error();
      const p = await response.json();
      if (p.HasPosition) {
        const point = [p.Latitude,p.Longitude];
        if (!map) map = showMap(shared).setView(point,15);
        if (!marker) marker = L.marker(point,{icon:L.divIcon({className:"",html:window.vehicleSymbol(p.Kind,p.Icon),iconSize:[26,26]})}).addTo(map);
        else {
          marker.setLatLng(point);
          if (!map.getBounds().contains(point)) map.panTo(point);
        }
        status.textContent = "Ostatnia pozycja: " + new Date(p.Time).toLocaleString("pl-PL",{timeZone:"Europe/Warsaw"});
      } else {
        if (map) { map.remove(); map = marker = null; }
        shared.hidden = true;
        status.textContent = "Oczekiwanie na ważną pozycję GPS.";
      }
    } catch (_) {
      status.textContent = "Brak połączenia. Ponawiam próbę.";
    }
    window.setTimeout(refresh,10000);
  }
  refresh();
}
})();
`
