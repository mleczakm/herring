(function () {
  'use strict';
  var root = document.getElementById('alert-settings');
  if (!root) return;
  var status = document.getElementById('alert-status');
  var form = document.getElementById('place-form');
  var list = document.getElementById('place-list');
  var suggestions = document.getElementById('place-suggestions');
  var isPrivate = root.dataset.private === 'true';
  var knownPlaces = [];

  var map, point, area;
  if (window.L) {
    map = L.map('place-map').setView([52.0, 19.3], 6);
    L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors', referrerPolicy: 'origin', maxZoom: 19
    }).addTo(map);
    map.on('click', function (event) {
      form.elements.latitude.value = event.latlng.lat.toFixed(6);
      form.elements.longitude.value = event.latlng.lng.toFixed(6);
      updateMap(false);
    });
  }
  function clearMap() {
    if (point) { map.removeLayer(point); point = null; }
    if (area) { map.removeLayer(area); area = null; }
  }
  function updateMap(center) {
    if (!map || !form.elements.latitude.value || !form.elements.longitude.value) return;
    var lat = Number(form.elements.latitude.value), lon = Number(form.elements.longitude.value);
    if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) return;
    var pos = [lat, lon], radius = Number(form.elements.radius_m.value) || 200;
    if (!point) point = L.circleMarker(pos, {radius: 6, color: '#a13920', fillOpacity: 1}).addTo(map);
    else point.setLatLng(pos);
    if (!area) area = L.circle(pos, {radius: radius, color: '#a13920', fillOpacity: .15}).addTo(map);
    else area.setLatLng(pos).setRadius(radius);
    if (center) map.setView(pos, 15);
  }
  ['latitude', 'longitude', 'radius_m'].forEach(function (name) {
    form.elements[name].addEventListener('input', function () { updateMap(name !== 'radius_m'); });
  });

  function say(message, error) {
    status.textContent = message;
    status.classList.toggle('error', !!error);
  }
  async function request(url, method, data) {
    var options = {method: method || 'GET', headers: {'Accept': 'application/json'}};
    if (data !== undefined) {
      options.headers['Content-Type'] = 'application/json';
      options.body = JSON.stringify(data);
    }
    var response = await fetch(url, options);
    if (!response.ok) throw new Error('request failed');
    if (response.status === 204) return null;
    var text = await response.text();
    return text ? JSON.parse(text) : null;
  }
  function textElement(tag, value) {
    var node = document.createElement(tag);
    node.textContent = value;
    return node;
  }
  async function loadPlaces() {
    var places = await request('/api/places');
    knownPlaces = places || [];
    document.querySelectorAll('.device-alert-form').forEach(function (deviceForm) {
      if (deviceForm.selectedPlaces) deviceForm.selectedPlaces = deviceForm.selectedPlaces.filter(function (id) { return knownPlaces.some(function (place) { return Number(place.id) === id; }); });
      renderPlaceChoices(deviceForm);
    });
    list.replaceChildren();
    if (!places || !places.length) {
      list.append(textElement('li', 'Nie masz jeszcze zapisanych miejsc.'));
      return;
    }
    places.forEach(function (place) {
      var row = document.createElement('li');
      var info = document.createElement('div');
      info.append(textElement('strong', place.name));
      info.append(textElement('small', Number(place.latitude).toFixed(5) + ', ' + Number(place.longitude).toFixed(5) + ' · ' + place.radius_m + ' m'));
      var remove = textElement('button', 'Usuń');
      remove.type = 'button';
      remove.setAttribute('aria-label', 'Usuń miejsce ' + place.name);
      remove.addEventListener('click', async function () {
        if (!confirm('Usunąć miejsce „' + place.name + '”?')) return;
        remove.disabled = true;
        try {
          await request('/api/places/' + encodeURIComponent(place.id), 'DELETE', {});
          await loadPlaces();
          await loadSuggestions();
          say('Miejsce zostało usunięte.');
        } catch (_) { say('Nie udało się usunąć miejsca. Spróbuj ponownie.', true); remove.disabled = false; }
      });
      row.append(info, remove);
      list.append(row);
    });
  }
  form.addEventListener('submit', async function (event) {
    event.preventDefault();
    var button = form.querySelector('button[type=submit]');
    button.disabled = true;
    var data = new FormData(form);
    try {
      await request('/api/places', 'POST', {name: data.get('name').trim(), latitude: Number(data.get('latitude')), longitude: Number(data.get('longitude')), radius_m: Number(data.get('radius_m'))});
      form.reset();
      clearMap();
      await loadPlaces();
      await loadSuggestions();
      say('Miejsce zostało dodane.');
    } catch (_) { say('Nie udało się dodać miejsca. Sprawdź dane i spróbuj ponownie.', true); }
    button.disabled = false;
  });
  async function loadSuggestions() {
    var heading = suggestions.querySelector('h3');
    suggestions.replaceChildren(heading);
    if (isPrivate) {
      suggestions.append(textElement('p', 'Propozycje są niedostępne w trybie prywatnym.'));
      return;
    }
    try {
      var items = await request('/api/place-suggestions');
      if (!items || !items.length) { suggestions.append(textElement('p', 'Brak propozycji z historii postojów.')); return; }
      var ul = document.createElement('ul');
      items.forEach(function (item) {
        var li = document.createElement('li');
        var button = textElement('button', Number(item.latitude).toFixed(5) + ', ' + Number(item.longitude).toFixed(5) + ' · dni z postojem: ' + item.visits);
        button.type = 'button'; button.className = 'suggestion-button';
        button.addEventListener('click', function () {
          form.elements.latitude.value = item.latitude;
          form.elements.longitude.value = item.longitude;
          updateMap(true);
          form.elements.name.focus();
          say('Współrzędne zostały wpisane. Nadaj miejscu nazwę i zapisz je.');
        });
        li.append(button); ul.append(li);
      });
      suggestions.append(ul);
    } catch (_) { suggestions.append(textElement('p', 'Nie udało się wczytać propozycji.')); }
  }
  loadPlaces().catch(function () { list.replaceChildren(textElement('li', 'Nie udało się wczytać miejsc.')); });
  loadSuggestions();
  function renderPlaceChoices(deviceForm) {
    var options = deviceForm.querySelector('.alert-place-options');
    var selected = deviceForm.selectedPlaces || [];
    options.replaceChildren();
    if (!knownPlaces.length) { options.textContent = 'Dodaj miejsce, aby je wybrać.'; return; }
    knownPlaces.forEach(function (place) {
      var label = document.createElement('label'); label.className = 'alert-check';
      var input = document.createElement('input'); input.type = 'checkbox'; input.value = place.id;
      input.checked = selected.indexOf(Number(place.id)) !== -1;
      input.addEventListener('change', function () {
        deviceForm.selectedPlaces = Array.from(options.querySelectorAll('input:checked'), function (item) { return Number(item.value); });
      });
      label.append(input, document.createTextNode(' ' + place.name)); options.append(label);
    });
  }
  document.querySelectorAll('.device-alert-form').forEach(function (deviceForm) {
    var url = '/api/devices/' + encodeURIComponent(deviceForm.dataset.deviceId) + '/alerts';
    var button = deviceForm.querySelector('button');
    request(url).then(function (data) {
      deviceForm.selectedPlaces = data.place_ids || [];
      renderPlaceChoices(deviceForm);
      deviceForm.elements.entry.checked = !!data.entry;
      deviceForm.elements.exit.checked = !!data.exit;
      deviceForm.elements.humor.checked = !!data.humor;
      deviceForm.elements.speed_kph.value = data.speed_kph || 0;
      button.disabled = false;
    }).catch(function () { say('Nie udało się wczytać ustawień urządzenia. Odśwież stronę i spróbuj ponownie.', true); });
    deviceForm.addEventListener('submit', async function (event) {
      event.preventDefault();
      if ((deviceForm.elements.entry.checked || deviceForm.elements.exit.checked) && !(deviceForm.selectedPlaces || []).length) {
        say('Wybierz przynajmniej jedno miejsce dla przyjazdów lub wyjazdów.', true);
        deviceForm.querySelector('.alert-place-choices').scrollIntoView({block: 'center'});
        return;
      }
      button.disabled = true;
      try {
        await request(url, 'PUT', {entry: deviceForm.elements.entry.checked, exit: deviceForm.elements.exit.checked, humor: deviceForm.elements.humor.checked, place_ids: deviceForm.selectedPlaces || [], speed_kph: Number(deviceForm.elements.speed_kph.value)});
        say('Ustawienia urządzenia zostały zapisane.');
      } catch (_) { say('Nie udało się zapisać ustawień urządzenia. Spróbuj ponownie.', true); }
      button.disabled = false;
    });
  });
  var pushStatus = document.getElementById('push-status');
  var enable = document.getElementById('push-enable');
  var disable = document.getElementById('push-disable');
  function applicationKey(key) {
    var raw = atob((key + '='.repeat((4 - key.length % 4) % 4)).replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(raw, function (char) { return char.charCodeAt(0); });
  }
  async function initPush() {
    if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
      pushStatus.textContent = 'Ta przeglądarka nie udostępnia powiadomień. Na iPhonie lub iPadzie dodaj Śledzia do ekranu początkowego i otwórz go stamtąd.'; return;
    }
    var config = await request('/api/push/config');
    if (!config || !config.enabled || !config.publicKey) { pushStatus.textContent = 'Powiadomienia nie są dostępne na tym serwerze.'; return; }
    await navigator.serviceWorker.register('/sw.js');
    var registration = await navigator.serviceWorker.ready;
    async function refresh() {
      var subscription = await registration.pushManager.getSubscription();
      enable.hidden = !!subscription; disable.hidden = !subscription;
      pushStatus.textContent = subscription ? 'Powiadomienia są włączone w tej przeglądarce.' : 'Powiadomienia są wyłączone w tej przeglądarce.';
    }
    enable.addEventListener('click', async function () {
      enable.disabled = true;
      var subscription;
      try {
        if (await Notification.requestPermission() !== 'granted') throw new Error('permission');
        subscription = await registration.pushManager.subscribe({userVisibleOnly: true, applicationServerKey: applicationKey(config.publicKey)});
        try { await request('/api/push/subscribe', 'POST', subscription.toJSON()); }
        catch (error) { await subscription.unsubscribe(); throw error; }
        await refresh();
      } catch (_) { pushStatus.textContent = 'Nie udało się włączyć powiadomień. Sprawdź zgodę w ustawieniach przeglądarki i spróbuj ponownie.'; }
      enable.disabled = false;
    });
    disable.addEventListener('click', async function () {
      disable.disabled = true;
      try {
        var subscription = await registration.pushManager.getSubscription();
        if (subscription) { await request('/api/push/unsubscribe', 'POST', {endpoint: subscription.endpoint}); await subscription.unsubscribe(); }
        await refresh();
      } catch (_) { pushStatus.textContent = 'Nie udało się wyłączyć powiadomień. Spróbuj ponownie.'; }
      disable.disabled = false;
    });
    await refresh();
  }
  initPush().catch(function () { pushStatus.textContent = 'Nie udało się sprawdzić powiadomień. Odśwież stronę i spróbuj ponownie.'; });
}());
