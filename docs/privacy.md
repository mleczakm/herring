# Privacy mode and data retention

Both are configured on the **Ustawienia** page (`/ustawienia`).

## Privacy mode

In privacy mode the server is only a relay for location history. Trackers keep
sending every position to Herring, but the server holds each one only until a
single bound browser has downloaded it and stored it locally. Then the server
deletes it. History is readable only in that browser.

### Turning it on

1. Install Herring as an app on the device that should keep the history:
   **Install app** in Chrome or Edge on Android and desktop, or
   **Share → Add to Home Screen** in Safari on iOS and iPadOS.
2. Open the installed app, go to **Ustawienia**, and choose
   **Włącz tryb prywatny na tym urządzeniu**.

The button appears only inside the installed app. Browsers protect an
installed app's storage much better than a regular tab's: Safari, for
example, deletes storage of sites not on the home screen after seven days
without a visit. Herring also asks the browser to mark the storage as
persistent; the settings page shows whether the browser agreed.

When privacy mode is turned on, every position currently on the server is
transferred to the device and deleted from the server.

### What changes

| | Normal | Privacy mode |
| --- | --- | --- |
| Where history lives | Server (SQLite) | Bound browser (IndexedDB) |
| Server copy of a position | Until server retention removes it | Until the bound browser acknowledges it, or server retention removes it first |
| Map in other browsers and sessions | Positions shown | Tracker list only, no positions |
| Map in the bound browser | From the server | From local storage, also offline |
| Route history (**Historia tras**) | `GET /api/history`, in any signed-in browser | Read from IndexedDB in the bound browser only; `GET /api/history` answers 409 `privacy_mode` |
| Place and speed alerts, place suggestions | Active | Paused; alert state resets when the mode changes, so movement during privacy mode triggers no late alerts |
| Offline alerts and positions by SMS | Active | Active; they carry no location, but an SMS position passes through the mobile operator and the SMS gateway ([details](offline-and-sms.md)) |

Server logs never contain coordinates, in either mode.

Tracker metadata (name, model, SIM number, configuration state, SMS command
history) stays on the server in both modes, as do saved places and alert
rules from **Miejsca i powiadomienia**. Herring needs it to configure
trackers and link incoming data.

### How the transfer works

- Binding: the browser generates a random 256-bit secret, keeps it in
  IndexedDB, and sends it once to `POST /api/privacy/enable`. The server stores
  only its SHA-256 hash. Binding another browser replaces the previous binding.
- Every five seconds while the dashboard is open, and whenever the settings or
  route history page opens or loads a route, the browser calls `GET /api/privacy/sync` with the secret in the
  `X-Herring-Client` header. The response holds at most 500 positions, oldest
  first.
- The browser writes the batch to IndexedDB. Only after that transaction
  commits does it call `POST /api/privacy/ack`, which deletes exactly those
  positions from the server. A crash between the two steps causes a repeated
  transfer, which is harmless; it never loses data. The acknowledgement
  identifies each position by row ID, tracker ID and tracker time, because
  SQLite reuses row IDs once the newest rows are deleted.
- Tabs of the same browser take turns using the Web Locks API, so two tabs
  cannot acknowledge the same batch.
- Both endpoints also require a signed-in session. JSON endpoints reject
  cross-origin requests and non-JSON bodies.

### Risks to accept

- **Losing the device loses the history.** The same applies to uninstalling the
  app, clearing site data, or the browser evicting storage. No copy exists on
  the server.
- **Signing out does not erase local history.** Anyone who can use the unlocked
  device and its browser profile can read the IndexedDB data with developer
  tools. Remove it with **Usuń dane z tej przeglądarki** before handing the
  device over.
- **Positions pass through the server.** The server decodes every frame and
  keeps it until the browser fetches it. If the bound device stays offline,
  positions accumulate on the server until server retention removes them.
- **Saved places stay on the server.** A place is coordinates you entered
  yourself, such as home, so it can reveal where a tracker usually is. Delete
  places you do not want kept on the server.
- **A signed-in session can rebind.** A session on another device can move
  privacy mode to itself. It then receives every position not yet fetched by
  the previous device. It cannot read history the previous device already
  holds.

### Moving or turning it off

- **Move to another device**: open the installed app there and choose
  **Przenieś tryb prywatny na to urządzenie**. History already on the old device
  stays there; that device shows it as no longer bound and offers to delete it.
- **Turn off from the bound device**: **Wyłącz tryb prywatny i usuń dane z tej
  przeglądarki**. Local history is deleted and does not return to the server.
- **Turn off from another device**: **Wyłącz tryb prywatny**. History stays on
  the bound device until it is deleted there.

In both cases the server starts keeping new positions again.

## Data retention

Two independent periods, in days. `0` means unlimited, the maximum is 3650.

- **Na serwerze**: positions the server received longer ago than this are
  deleted. This runs when the settings are saved, at startup, and hourly. In
  privacy mode it limits how long undelivered positions wait for the bound
  device.
- **W przeglądarce**: the bound browser deletes positions it received longer
  ago than this, after every sync. It only matters in privacy mode, the only
  mode where the browser stores history.

Retention covers positions, including their raw frames. Trackers, users,
sessions and SMS command history are not affected.

## Installable app

Herring serves a web manifest (`/manifest.webmanifest`) and a service worker
(`/sw.js`). The service worker caches the application shell so the installed
app opens offline. It never caches `/api/` responses, and it forgets cached
pages when the login page loads. The same worker shows Web Push notifications.
Bump `CACHE` in `internal/httpapi/pwa.go` whenever a precached asset URL
changes, for example the `?v=` of `dashboard.js`.
