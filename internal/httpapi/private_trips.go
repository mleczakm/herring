package httpapi

const privateTripsJS = `(function(){
"use strict";
const root = document.getElementById("private-trips");
const status = document.getElementById("private-trips-status");
const privacy = window.HerringPrivacy;
const zone = "Europe/Warsaw";
const dateTime = new Intl.DateTimeFormat("pl-PL",{timeZone:zone,dateStyle:"short",timeStyle:"short"});
const dayFormat = new Intl.DateTimeFormat("sv-SE",{timeZone:zone,year:"numeric",month:"2-digit",day:"2-digit"});

// Keep these thresholds aligned with sqlite.Trips. Private fixes stay in IndexedDB.
function split(points) {
  const trips = [];
  let previous, trip, waiting = [], stopped;
  function finish() {
    const result = trip && trip.meters >= 25 && trip.points.length >= 2 ? trip : undefined;
    if (result) trips.push(result);
    trip = undefined;
    waiting = [];
    return result;
  }
  for (const point of points) {
    const gap = previous ? point.tracker_ms - previous.tracker_ms : Infinity;
    if (gap <= 0) continue;
    if (gap > 600000) { finish(); previous = stopped = undefined; }
    const still = point.speed_kph < 3;
    if (still && !trip) {
      if (stopped) stopped.stop = point.tracker_ms - stopped.points.at(-1).tracker_ms;
      previous = point;
      continue;
    }
    stopped = undefined;
    if (still && waiting.length && point.tracker_ms - waiting[0].tracker_ms >= 300000) {
      trip.stop = point.tracker_ms - waiting[0].tracker_ms;
      stopped = finish();
      previous = point;
      continue;
    }
    if (!trip) trip = {points:previous && gap <= 60000 ? [previous] : [],meters:0,moving:0,top:0,stop:0};
    if (trip.points.length && !(previous.speed_kph < 3 && still)) {
      const rad = Math.PI / 180;
      const a = Math.sin((point.latitude - previous.latitude) * rad / 2);
      const b = Math.sin((point.longitude - previous.longitude) * rad / 2);
      const meters = 12742000 * Math.asin(Math.sqrt(Math.min(1,a*a + Math.cos(previous.latitude*rad)*Math.cos(point.latitude*rad)*b*b)));
      if (meters >= 5 && meters / gap * 3600 <= 250) {
        trip.meters += meters;
        if (gap <= 120000) trip.moving += gap;
      }
    }
    if (still) {
      if (!waiting.length) trip.points.push(point);
      waiting.push(point);
    } else {
      for (const idle of waiting.slice(1)) trip.points.push(idle);
      waiting = [];
      trip.points.push(point);
      trip.top = Math.max(trip.top,point.speed_kph);
    }
    previous = point;
  }
  finish();
  return trips.reverse();
}

function add(parent,tag,text) {
  const element = document.createElement(tag);
  if (text !== undefined) element.textContent = text;
  parent.appendChild(element);
  return element;
}
function button(parent,text,action) {
  const element = add(parent,"button",text);
  element.type = "button";
  element.className = "btn btn-small";
  element.addEventListener("click",action);
  return element;
}
function download(name,type,body) {
  const url = URL.createObjectURL(new Blob([body],{type}));
  const link = document.createElement("a");
  link.download = name;
  link.href = url;
  link.click();
  window.setTimeout(() => URL.revokeObjectURL(url),1000);
}
function totals(group) {
  return group.reduce((a,t) => ({meters:a.meters+t.meters,moving:a.moving+t.moving}),{meters:0,moving:0});
}
function duration(ms) { return Math.round(ms/60000); }
function caption(group) {
  const sum = totals(group);
  return group.length + " przejazdów · " + (sum.meters/1000).toFixed(1) + " km · " + duration(sum.moving) + " min w ruchu";
}
function groupTrips(trips,period) {
  const groups = new Map();
  for (const trip of trips) {
    const key = dayFormat.format(trip.points[0].tracker_ms).slice(0,period);
    if (!groups.has(key)) groups.set(key,[]);
    groups.get(key).push(trip);
  }
  return groups;
}
function csvField(value) {
  let text = String(value);
  if (/^\s*[=+@-]/.test(text)) text = "'" + text;
  return '"' + text.replaceAll('"','""') + '"';
}

async function main() {
  if (!root || !privacy || !window.indexedDB) {
    status.textContent = "Lokalna historia jest dostępna w przeglądarce powiązanej z trybem prywatnym.";
    return;
  }
  const sync = await privacy.sync().catch(() => ({status:"offline"}));
  if (["no_key","not_bound","privacy_disabled"].includes(sync.status)) {
    status.textContent = "Otwórz dziennik w przeglądarce powiązanej z trybem prywatnym.";
    return;
  }
  const trips = split(await privacy.trackPoints(+root.dataset.device,+root.dataset.from,+root.dataset.until));
  status.textContent = (sync.status !== "ok" ? "Odczyt z tego urządzenia. " : "") + (trips.length ? caption(trips) : "Brak wykrytych przejazdów.");
  if (!trips.length) return;

  button(root,"Pobierz CSV",async () => {
    const rows = [["Początek (Europe/Warsaw)","Koniec (Europe/Warsaw)","Dystans (km)","Czas ruchu (min)","Maks. prędkość (km/h)","Notatka"]];
    for (const trip of trips) {
      const first = trip.points[0], last = trip.points.at(-1);
      rows.push([dateTime.format(first.tracker_ms),dateTime.format(last.tracker_ms),(trip.meters/1000).toFixed(3),duration(trip.moving),trip.top,await privacy.tripNote(first.key) || ""]);
    }
    download("przejazdy.csv","text/csv;charset=utf-8",rows.map(row => row.map(csvField).join(",")).join("\r\n"));
  });
  for (const [period,label] of [[10,"Statystyki dzienne"],[7,"Statystyki miesięczne"]]) {
    const details = add(root,"details");
    add(details,"summary",label);
    for (const [key,group] of groupTrips(trips,period)) add(details,"p",key + " · " + caption(group));
  }
  const stops = new Map();
  for (const trip of trips) {
    if (!trip.stop) continue;
    const point = trip.points.at(-1);
    const location = point.latitude.toFixed(3) + ", " + point.longitude.toFixed(3);
    const stop = stops.get(location) || {count:0,duration:0};
    stop.count++;
    stop.duration += trip.stop;
    stops.set(location,stop);
  }
  if (stops.size) {
    const details = add(root,"details");
    add(details,"summary","Najczęstsze postoje");
    for (const [location,stop] of [...stops].sort((a,b) => b[1].count-a[1].count || b[1].duration-a[1].duration).slice(0,10)) {
      add(details,"p",location + " · " + stop.count + " · " + duration(stop.duration) + " min");
    }
  }
  for (const trip of trips) {
    const first = trip.points[0], last = trip.points.at(-1);
    const item = add(root,"details");
    add(item,"summary",dateTime.format(first.tracker_ms) + " – " + dateTime.format(last.tracker_ms));
    add(item,"p",(trip.meters/1000).toFixed(2) + " km · " + duration(trip.moving) + " min w ruchu · maks. " + Math.round(trip.top) + " km/h" + (trip.stop ? " · postój " + duration(trip.stop) + " min" : ""));
    let map;
    const mapButton = button(item,"Pokaż trasę",() => {
      if (!map) {
        element.hidden = false;
        map = L.map(element);
        // A local route can be viewed without requesting map tiles for its area.
        const line = L.polyline(trip.points.map(p => [p.latitude,p.longitude])).addTo(map);
        map.fitBounds(line.getBounds(),{padding:[16,16],maxZoom:17});
      } else {
        element.hidden = !element.hidden;
        if (!element.hidden) map.invalidateSize();
      }
      mapButton.textContent = element.hidden ? "Pokaż trasę" : "Ukryj trasę";
    });
    button(item,"Pobierz GPX",() => {
      const points = trip.points.map(p => '<trkpt lat="' + p.latitude + '" lon="' + p.longitude + '"><time>' + new Date(p.tracker_ms).toISOString() + '</time></trkpt>');
      download("przejazd.gpx","application/gpx+xml",'<?xml version="1.0" encoding="UTF-8"?><gpx xmlns="http://www.topografix.com/GPX/1/1" version="1.1" creator="Śledź"><trk><trkseg>' + points.join("") + '</trkseg></trk></gpx>');
    });
    const element = add(item,"div");
    element.dataset.trackMap = "";
    element.hidden = true;
    const label = add(item,"label","Notatka lub cel przejazdu");
    const input = add(label,"input");
    input.maxLength = 300;
    input.value = await privacy.tripNote(first.key) || "";
    const saveStatus = add(item,"p");
    saveStatus.setAttribute("role","status");
    button(item,"Zapisz notatkę",async () => {
      try {
        const saved = await privacy.tripNote(first.key,input.value.trim());
        saveStatus.textContent = saved ? "Zapisano na tym urządzeniu." : "Pozycja początkowa nie jest już dostępna.";
      } catch (_) {
        saveStatus.textContent = "Nie udało się zapisać notatki.";
      }
    });
  }
}

main().catch(() => { status.textContent = "Nie udało się odczytać lokalnych przejazdów."; });
})();
`
