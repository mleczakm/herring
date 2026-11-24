package httpapi

// webManifest makes Herring installable. Privacy mode requires the installed
// app: browsers keep an installed app's storage far more reliably than a
// regular tab's (Safari evicts script-written storage of sites that are not
// on the home screen after seven days without a visit).
const webManifest = `{
  "name": "Śledź",
  "short_name": "Śledź",
  "description": "Mapa lokalizatorów GPS",
  "lang": "pl",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#0e3332",
  "theme_color": "#0e3332",
  "icons": [
    {"src": "/assets/icon-192.png?v=2", "sizes": "192x192", "type": "image/png"},
    {"src": "/assets/icon-512.png?v=2", "sizes": "512x512", "type": "image/png"},
    {"src": "/assets/icon-maskable-512.png?v=2", "sizes": "512x512", "type": "image/png", "purpose": "maskable"},
    {"src": "/assets/herring.svg?v=2", "sizes": "any", "type": "image/svg+xml"}
  ]
}
`

// serviceWorkerJS caches the application shell so an installed app opens
// offline and can still show positions kept in privacy mode. API responses are
// never cached: in privacy mode the only copy of history is in IndexedDB.
//
// Bump CACHE whenever a precached asset changes. Assets are cache-first and
// served as immutable, so a changed file also needs a new ?v= in its URL.
const serviceWorkerJS = `"use strict";
var CACHE = "sledz-shell-v6";
var ASSETS = [
  "/assets/herring.css?v=6",
  "/assets/herring.svg?v=2",
  "/assets/leaflet.css",
  "/assets/leaflet.js",
  "/assets/dashboard.js?v=6",
  "/assets/devices.js?v=1",
  "/assets/privacy.js?v=4",
  "/assets/history.js?v=1",
  "/assets/vehicle-maps.js?v=1",
  "/assets/private-trips.js?v=1",
  "/assets/settings.js?v=1",
  "/assets/space-grotesk-latin.woff2",
  "/assets/space-grotesk-latin-ext.woff2"
];
var PAGES = ["/", "/urzadzenia", "/historia", "/ustawienia"];

self.addEventListener("install", function(event){
  event.waitUntil(caches.open(CACHE).then(function(cache){ return cache.addAll(ASSETS); }).then(function(){ return self.skipWaiting(); }));
});

self.addEventListener("activate", function(event){
  event.waitUntil(caches.keys().then(function(keys){
    return Promise.all(keys.filter(function(key){ return key !== CACHE; }).map(function(key){ return caches.delete(key); }));
  }).then(function(){ return self.clients.claim(); }));
});

self.addEventListener("fetch", function(event){
  var request = event.request;
  if (request.method !== "GET") return;
  var url = new URL(request.url);
  if (url.pathname.startsWith("/lokalizacja/")) return;
  if (url.origin !== self.location.origin) return;
  if (url.pathname.indexOf("/api/") === 0 || url.pathname.indexOf("/webhooks/") === 0) return;

  if (request.mode === "navigate") {
    event.respondWith(fetch(request).then(function(response){
      var path = new URL(response.url || request.url).pathname;
      if (path === "/login" || path === "/setup") {
        // Signed out: forget cached signed-in pages.
        caches.open(CACHE).then(function(cache){ PAGES.forEach(function(page){ cache.delete(page); }); });
      } else if (response.ok && PAGES.indexOf(path) !== -1) {
        var copy = response.clone();
        caches.open(CACHE).then(function(cache){ cache.put(path, copy); });
      }
      return response;
    }).catch(function(){
      return caches.match(url.pathname).then(function(cached){ return cached || Response.error(); });
    }));
    return;
  }

  event.respondWith(caches.match(request).then(function(cached){ return cached || fetch(request); }));
});
`

// privacyJS is the browser side of privacy mode, shared by the dashboard and
// the settings page as window.HerringPrivacy. Positions are written to
// IndexedDB and only then acknowledged, so the server deletes nothing the
// browser has not durably stored.
const privacyJS = `(function(){
"use strict";
var DB_NAME = "herring-private";
var MAX_BATCHES = 40;
var dbPromise = null;

if ("serviceWorker" in navigator) {
  window.addEventListener("load", function(){ navigator.serviceWorker.register("/sw.js").catch(function(){}); });
}

function request(r){ return new Promise(function(resolve, reject){ r.onsuccess = function(){ resolve(r.result); }; r.onerror = function(){ reject(r.error); }; }); }
function finished(tx){ return new Promise(function(resolve, reject){ tx.oncomplete = function(){ resolve(); }; tx.onerror = tx.onabort = function(){ reject(tx.error || new Error("transaction aborted")); }; }); }

function db(){
  if (!dbPromise) {
    dbPromise = new Promise(function(resolve, reject){
      var open = indexedDB.open(DB_NAME, 1);
      open.onupgradeneeded = function(){
        var d = open.result;
        d.createObjectStore("meta");
        d.createObjectStore("devices", {keyPath: "id"});
        var positions = d.createObjectStore("positions", {keyPath: "key"});
        positions.createIndex("received", "received_ms");
        positions.createIndex("device_time", ["device_id", "tracker_ms"]);
      };
      open.onsuccess = function(){
        var d = open.result;
        // Let another tab delete the database (wipe) instead of blocking it.
        d.onversionchange = function(){ d.close(); dbPromise = null; };
        resolve(d);
      };
      open.onerror = function(){ reject(open.error); };
    });
  }
  return dbPromise;
}
function getMeta(key){ return db().then(function(d){ return request(d.transaction("meta").objectStore("meta").get(key)); }); }
function putMeta(values){
  return db().then(function(d){
    var tx = d.transaction("meta", "readwrite");
    Object.keys(values).forEach(function(key){
      if (values[key] === undefined) tx.objectStore("meta").delete(key);
      else tx.objectStore("meta").put(values[key], key);
    });
    return finished(tx);
  });
}

function installed(){
  var modes = ["standalone", "fullscreen", "minimal-ui", "window-controls-overlay"];
  for (var i = 0; i < modes.length; i++) if (window.matchMedia("(display-mode: " + modes[i] + ")").matches) return true;
  return navigator.standalone === true;
}
function browserLabel(){
  var ua = navigator.userAgent;
  var browser = /Edg\//.test(ua) ? "Edge" : /OPR\//.test(ua) ? "Opera" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Przeglądarka";
  var os = /Android/.test(ua) ? "Android" : /iPhone|iPad|iPod/.test(ua) ? "iOS" : /Mac OS X/.test(ua) ? "macOS" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "";
  return os ? browser + " · " + os : browser;
}
function randomKey(){
  var bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return Array.prototype.map.call(bytes, function(b){ return ("0" + b.toString(16)).slice(-2); }).join("");
}
function postJSON(url, body, key){
  var headers = {"Content-Type": "application/json"};
  if (key) headers["X-Herring-Client"] = key;
  return fetch(url, {method: "POST", headers: headers, body: JSON.stringify(body || {}), credentials: "same-origin"});
}
function errorCode(response){
  return response.json().then(function(body){ return body.error || "http_" + response.status; }, function(){ return "http_" + response.status; });
}

// enable binds privacy mode to this browser and pulls every position the
// server holds. The secret never leaves this browser except in requests.
function enable(){
  if (!installed()) return Promise.reject(new Error("not_installed"));
  var key = randomKey(), label = browserLabel();
  return putMeta({client_key: key, label: label}).then(function(){
    return postJSON("/api/privacy/enable", {client_key: key, label: label});
  }).then(function(response){
    if (!response.ok) {
      return putMeta({client_key: undefined, label: undefined}).then(function(){ throw new Error("enable_failed_" + response.status); });
    }
    return putMeta({bound_at: new Date().toISOString()});
  }).then(function(){
    return navigator.storage && navigator.storage.persist ? navigator.storage.persist() : false;
  }).then(function(persisted){
    return sync().then(function(result){ result.persisted = persisted; return result; });
  });
}

// disable turns privacy mode off on the server and erases this browser's copy.
function disable(){
  return postJSON("/api/privacy/disable", {}).then(function(response){
    if (!response.ok) throw new Error("disable_failed_" + response.status);
    return wipe();
  });
}

function syncOnce(key){
  return fetch("/api/privacy/sync", {headers: {"X-Herring-Client": key, "Accept": "application/json"}, credentials: "same-origin", cache: "no-store"}).then(function(response){
    if (!response.ok) return errorCode(response).then(function(code){ return {status: code}; });
    return response.json().then(function(data){
      return db().then(function(d){
        var tx = d.transaction(["devices", "positions", "meta"], "readwrite");
        var devices = tx.objectStore("devices");
        devices.clear();
        data.devices.forEach(function(device){ devices.put(device); });
        var positions = tx.objectStore("positions");
        data.positions.forEach(function(p){
          positions.put({
            key: p.device_id + "|" + p.tracker_time, device_id: p.device_id,
            tracker_time: p.tracker_time, tracker_ms: Date.parse(p.tracker_time),
            received_at: p.received_at, received_ms: Date.parse(p.received_at),
            gps_valid: p.gps_valid, latitude: p.latitude, longitude: p.longitude, speed_kph: p.speed_kph, heading: p.heading,
            source: p.source || "gprs"
          });
        });
        tx.objectStore("meta").put(data.browser_retention_days, "retention_days");
        tx.objectStore("meta").put(new Date().toISOString(), "last_sync");
        return finished(tx);
      }).then(function(){
        if (!data.positions.length) return {status: "ok", received: 0, more: false};
        var keys = data.positions.map(function(p){ return {id: p.id, device_id: p.device_id, tracker_time: p.tracker_time}; });
        return postJSON("/api/privacy/ack", {positions: keys}, key).then(function(response){
          if (!response.ok) return errorCode(response).then(function(code){ return {status: code}; });
          return {status: "ok", received: data.positions.length, more: data.more};
        });
      });
    });
  });
}

// sync transfers positions until the server has none left for this browser,
// then applies the browser retention period. Tabs take turns via Web Locks.
function sync(){
  return getMeta("client_key").then(function(key){
    if (!key) return {status: "no_key"};
    var run = function(){
      var total = 0, batches = 0;
      function next(){
        return syncOnce(key).then(function(result){
          if (result.status !== "ok") return result;
          total += result.received;
          if (result.more && ++batches < MAX_BATCHES) return next();
          return applyRetention().then(function(removed){ return {status: "ok", received: total, removed: removed}; });
        });
      }
      return next();
    };
    return navigator.locks && navigator.locks.request ? navigator.locks.request("herring-privacy-sync", run) : run();
  });
}

function applyRetention(){
  return getMeta("retention_days").then(function(days){
    if (!days) return 0;
    var cutoff = Date.now() - days * 86400000;
    return db().then(function(d){
      var tx = d.transaction(["positions","meta"], "readwrite");
      var removed = 0;
      tx.objectStore("positions").index("received").openCursor(IDBKeyRange.upperBound(cutoff, true)).onsuccess = function(event){
        var cursor = event.target.result;
        if (cursor) { tx.objectStore("meta").delete("trip-note:" + cursor.primaryKey); cursor.delete(); removed++; cursor.continue(); }
      };
      return finished(tx).then(function(){ return removed; });
    });
  });
}

// devices returns the locally known trackers with their newest local
// position, in the same shape as GET /api/positions.
function devices(){
  return db().then(function(d){
    return request(d.transaction("devices").objectStore("devices").getAll()).then(function(list){
      var tx = d.transaction("positions");
      var index = tx.objectStore("positions").index("device_time");
      list.sort(function(a, b){ return b.id - a.id; });
      return Promise.all(list.map(function(device){
        var item = {id: device.id, name: device.name, model: device.model, config_status: device.config_status, config_detail: device.config_detail, vehicle_type: device.vehicle_type, map_icon: device.map_icon, has_position: false};
        if (!device.tracker_id) return item;
        var range = IDBKeyRange.bound([device.tracker_id, -Infinity], [device.tracker_id, Infinity]);
        return request(index.openCursor(range, "prev")).then(function(cursor){
          if (!cursor) return item;
          var p = cursor.value;
          item.has_position = true;
          item.latitude = p.latitude; item.longitude = p.longitude; item.speed_kph = p.speed_kph; item.heading = p.heading;
          item.gps_valid = p.gps_valid; item.tracker_time = p.tracker_time; item.received_at = p.received_at;
          item.source = p.source || "gprs";
          return item;
        });
      }));
    });
  });
}

// route returns the stored positions of one tracker (by its dashboard ID)
// with tracker time in [fromMs, toMs), oldest first, in the shape of
// GET /api/history, plus the oldest position kept for that tracker.
function route(deviceID, fromMs, toMs){
  return db().then(function(d){
    return request(d.transaction("devices").objectStore("devices").get(Number(deviceID))).then(function(device){
      var result = {positions: [], oldest: null};
      if (!device || !device.tracker_id) return result;
      var tx = d.transaction("positions");
      var index = tx.objectStore("positions").index("device_time");
      var all = IDBKeyRange.bound([device.tracker_id, -Infinity], [device.tracker_id, Infinity]);
      var range = IDBKeyRange.bound([device.tracker_id, fromMs], [device.tracker_id, toMs], false, true);
      return Promise.all([request(index.getAll(range)), request(index.openCursor(all))]).then(function(values){
        result.positions = values[0].map(function(p){
          return {gps_valid: p.gps_valid, tracker_time: p.tracker_time, received_at: p.received_at, latitude: p.latitude, longitude: p.longitude, speed_kph: p.speed_kph, heading: p.heading, source: p.source || "gprs"};
        });
        result.oldest = values[1] ? values[1].value.tracker_time : null;
        return result;
      });
    });
  });
}

function stats(){
  return db().then(function(d){
    var tx = d.transaction(["positions", "meta"]);
    var store = tx.objectStore("positions");
    var meta = tx.objectStore("meta");
    return Promise.all([
      request(store.count()), request(store.index("received").openCursor()),
      request(meta.get("client_key")), request(meta.get("bound_at")), request(meta.get("last_sync")), request(meta.get("retention_days"))
    ]).then(function(values){
      return {count: values[0], oldest: values[1] ? values[1].value.received_at : null, hasKey: !!values[2], boundAt: values[3] || null, lastSync: values[4] || null, retentionDays: values[5] || 0};
    });
  });
}

// wipe deletes every position and the binding secret from this browser.
function wipe(){
  return db().then(function(d){
    d.close();
    dbPromise = null;
    return new Promise(function(resolve, reject){
      var r = indexedDB.deleteDatabase(DB_NAME);
      r.onsuccess = function(){ resolve(); };
      r.onerror = function(){ reject(r.error); };
      // onblocked: other tabs close their connection on versionchange, after
      // which deletion completes and onsuccess fires.
    });
  });
}

async function trackPoints(id, from, until){
  const d = await db();
  const device = await request(d.transaction("devices").objectStore("devices").get(id));
  if (!device || !device.tracker_id) return [];
  const index = d.transaction("positions").objectStore("positions").index("device_time");
  const range = IDBKeyRange.bound([device.tracker_id,from],[device.tracker_id,until],false,true);
  return (await request(index.getAll(range))).filter(p => p.gps_valid && p.source !== "sms");
}
async function tripNote(key, note){
  if (note === undefined) return getMeta("trip-note:" + key);
  const d = await db();
  const tx = d.transaction(["meta","positions"],"readwrite");
  const complete = finished(tx);
  const point = await request(tx.objectStore("positions").get(key));
  if (point) tx.objectStore("meta").put(note,"trip-note:" + key);
  await complete;
  return !!point;
}

window.HerringPrivacy = {trackPoints: trackPoints, tripNote: tripNote, installed: installed, enable: enable, disable: disable, sync: sync, devices: devices, route: route, stats: stats, wipe: wipe};
})();
`
