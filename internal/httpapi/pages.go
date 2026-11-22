package httpapi

import _ "embed"

//go:embed public.html
var publicPages string

// pages holds the dashboard and shared template components. Assets (fonts, Leaflet, dashboard
// JS) are self-hosted (see assets/ and dashboardJS) instead of pulled from a
// CDN at runtime, so the strict CSP in securityHeaders never needs to trust a
// third party for script execution — only map tile images are cross-origin.
const pages = `
{{define "head"}}<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#152e33"><link rel="icon" href="/assets/herring.svg" type="image/svg+xml"><link rel="manifest" href="/manifest.webmanifest"><link rel="apple-touch-icon" href="/assets/icon-180.png"><link rel="stylesheet" href="/assets/herring.css">{{end}}



{{define "icon-map"}}<svg class="icon" viewBox="0 0 24 24"><path d="M9 4 3 6.5v14L9 18l6 2.5 6-2.5v-14L15 6.5 9 4Z"/><path d="M9 4v14M15 6.5v14"/></svg>{{end}}
{{define "icon-crosshair"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="12" cy="12" r="6.5"/><path d="M12 2v4M12 18v4M2 12h4M18 12h4"/></svg>{{end}}
{{define "icon-path"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="5" cy="19" r="2"/><circle cx="19" cy="5" r="2"/><path d="M7 19h6a4 4 0 0 0 4-4V9a4 4 0 0 1 2-3.5"/></svg>{{end}}
{{define "icon-geofence"}}<svg class="icon" viewBox="0 0 24 24"><rect x="4" y="6" width="16" height="12" rx="2"/><circle cx="4" cy="6" r="1.4" fill="currentColor" stroke="none"/><circle cx="20" cy="6" r="1.4" fill="currentColor" stroke="none"/><circle cx="4" cy="18" r="1.4" fill="currentColor" stroke="none"/><circle cx="20" cy="18" r="1.4" fill="currentColor" stroke="none"/></svg>{{end}}
{{define "icon-bell"}}<svg class="icon" viewBox="0 0 24 24"><path d="M6 10a6 6 0 1 1 12 0c0 4 1.5 5.5 1.5 5.5H4.5S6 14 6 10Z"/><path d="M9.5 18.5a2.5 2.5 0 0 0 5 0"/></svg>{{end}}
{{define "icon-gear"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="12" cy="12" r="3.2"/><path d="M12 3v2.2M12 18.8V21M4.9 4.9l1.6 1.6M17.5 17.5l1.6 1.6M3 12h2.2M18.8 12H21M4.9 19.1l1.6-1.6M17.5 6.5l1.6-1.6"/></svg>{{end}}
{{define "icon-list"}}<svg class="icon" viewBox="0 0 24 24"><path d="M4 6h16M4 12h16M4 18h16"/></svg>{{end}}
{{define "icon-search"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m20 20-4.35-4.35"/></svg>{{end}}
{{define "icon-plus"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/></svg>{{end}}
{{define "icon-user"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="12" cy="8" r="3.5"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0"/></svg>{{end}}
{{define "icon-caret"}}<svg class="icon" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg>{{end}}
{{define "icon-x"}}<svg class="icon" viewBox="0 0 24 24"><path d="M6 6l12 12M18 6 6 18"/></svg>{{end}}
{{define "icon-activity"}}<svg class="icon" viewBox="0 0 24 24"><path d="M3 12h4l2 8 4-16 2 8h6"/></svg>{{end}}
{{define "icon-clock"}}<svg class="icon" viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3.5 2"/></svg>{{end}}
{{define "icon-logout"}}<svg class="icon" viewBox="0 0 24 24"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5M21 12H9"/></svg>{{end}}

{{define "brand-mark"}}<img class="brand-mark" src="/assets/herring.svg" width="44" height="44" alt="">{{end}}
{{define "wordmark"}}{{template "brand-mark"}}<span class="wordmark">Śledź</span>{{end}}

{{define "sidebar"}}<aside class="sidebar" id="sidebar">
    <a class="sidebar-brand" href="/o-projekcie">{{template "brand-mark"}}<div><span class="wordmark">Śledź</span><small>Panel lokalizacji</small></div></a>
    <nav class="sidebar-nav"><ul>
      <li><a class="nav-link{{if eq .Active "map"}} active{{end}}" href="/">{{template "icon-map"}}<span>Mapa na żywo</span></a></li>
      <li><a class="nav-link" href="{{if eq .Active "map"}}#device-search{{else}}/{{end}}">{{template "icon-crosshair"}}<span>Urządzenia</span></a></li>
      <li><span class="nav-link planned">{{template "icon-path"}}<span>Historia Tras</span><span class="soon">wkrótce</span></span></li>
      <li><a class="nav-link" href="/powiadomienia">{{template "icon-bell"}}<span>Miejsca i powiadomienia</span></a></li>
      <li><a class="nav-link{{if eq .Active "settings"}} active{{end}}" href="/ustawienia">{{template "icon-gear"}}<span>Ustawienia</span>{{if .Privacy}}<span class="badge-private">prywatny</span>{{end}}</a></li>
    </ul></nav>
    <div class="sidebar-foot"><a class="nav-link" href="/o-projekcie">{{template "icon-crosshair"}}<span>O Śledziu</span></a></div>
  </aside>{{end}}

{{define "home"}}<!doctype html><html lang="pl"><head><title>Mapa na żywo · Śledź</title><link rel="stylesheet" href="/assets/leaflet.css">{{template "head"}}</head><body{{if .Privacy}} data-privacy="local"{{end}}>
<div class="app">
  <div class="sidebar-backdrop" id="sidebar-backdrop"></div>
  {{template "sidebar" .}}
  <main class="content">
    <header class="topbar">
      <button class="btn-icon menu-toggle glass" id="menu-toggle" aria-controls="sidebar" aria-expanded="false" aria-label="Menu nawigacji" type="button">{{template "icon-list"}}</button>
      <h1 class="map-title">{{template "icon-map"}} Mapa na żywo</h1>
      <div class="user-menu">
        <button class="user-pill glass" id="user-menu-toggle" type="button"><span class="user-avatar">{{template "icon-user"}}</span><span>{{.User.DisplayName}}</span>{{template "icon-caret"}}</button>
        <div class="user-dropdown" id="user-dropdown"><form method="post" action="/logout"><button type="submit">{{template "icon-logout"}}Wyloguj</button></form></div>
      </div>
    </header>
    <div id="map"></div>
    <section class="device-panel glass" id="device-panel">
      <div class="device-panel-head">
        <h2>{{template "icon-crosshair"}}Moje urządzenia</h2>
        <div class="search">{{template "icon-search"}}<input type="text" id="device-search" aria-label="Szukaj urządzenia" placeholder="Szukaj urządzenia…"></div>
      </div>
      <div class="device-list" id="device-list" data-empty-html="{{if not .Devices}}1{{end}}">
        {{if not .Devices}}<div class="empty-state">{{template "brand-mark"}}<strong>Dodaj pierwszy lokalizator</strong><p>Po odebraniu sygnału GPS jego pozycja pojawi się na mapie.</p></div>
        {{else}}{{range .Devices}}<button type="button" class="device-item" data-id="{{.ID}}"><span class="device-avatar" style="background:var(--offline)">{{template "icon-crosshair"}}</span><span class="device-info"><strong>{{if .Name}}{{.Name}}{{else}}{{.PhoneNumber}}{{end}}</strong><span class="device-meta"><span data-role="summary">{{if eq .ConfigStatus "configured"}}Oczekiwanie na sygnał…{{else if eq .ConfigStatus "failed"}}⚠ Konfiguracja nieudana{{else}}⏳ Konfigurowanie…{{end}}</span></span></span></button>{{end}}
        {{end}}
      </div>
      <div class="device-panel-foot"><button class="btn" id="open-add-device" type="button">{{template "icon-plus"}}Dodaj lokalizator</button></div>
    </section>
    <section class="add-device-panel glass" id="add-device-panel">
      <div class="add-device-panel-head"><h2>Dodaj lokalizator</h2><button class="btn-icon" id="close-add-device" aria-label="Zamknij formularz" type="button">{{template "icon-x"}}</button></div>
      {{if .Ready}}<form method="post" action="/devices"><label>Wariant<select name="model"><option value="st901-2g">ST-901 2G</option><option value="st901-4g">ST-901 4G</option></select></label><label>Numer telefonu karty SIM<input name="phone" type="tel" placeholder="+48…" required></label><label>Nazwa (opcjonalnie)<input name="name"></label><button class="btn" type="submit">Dodaj i skonfiguruj przez SMS</button></form>{{else}}<p class="hint">Aby dodać lokalizator, poproś administratora o włączenie wysyłki SMS.</p>{{end}}
    </section>
    <div class="toast glass" role="status" id="agent-toast">
      <span class="toast-avatar">{{template "brand-mark"}}</span>
      <div><h4>Agent Śledź melduje</h4><p id="toast-message"></p></div>
      <button class="btn-icon" id="toast-close" aria-label="Zamknij komunikat" type="button">{{template "icon-x"}}</button>
    </div>
  </main>
</div>
<script src="/assets/leaflet.js"></script>
<script src="/assets/privacy.js?v=1"></script>
<script src="/assets/dashboard.js?v=3"></script>
</body></html>{{end}}

{{define "settings"}}<!doctype html><html lang="pl"><head><title>Ustawienia · Śledź</title>{{template "head"}}</head><body{{if .Privacy}} data-privacy="local"{{end}}>
<div class="app">
  <div class="sidebar-backdrop" id="sidebar-backdrop"></div>
  {{template "sidebar" .}}
  <main class="content page">
    <header class="page-head">
      <button class="btn-icon menu-toggle" id="menu-toggle" aria-controls="sidebar" aria-expanded="false" aria-label="Menu nawigacji" type="button">{{template "icon-list"}}</button>
      <h1>{{template "icon-gear"}} Ustawienia</h1>
    </header>
    <div class="page-body">
      {{if .Saved}}<p class="notice" role="status">Zapisano ustawienia przechowywania danych.</p>{{end}}
      {{if .Error}}<p class="notice alert" role="alert">{{.Error}}</p>{{end}}
      <section class="card" id="privacy" aria-labelledby="privacy-title">
        <h2 id="privacy-title">Tryb prywatny</h2>
        <p>Gdy jest włączony, serwer przechowuje pozycje tylko do chwili, gdy zainstalowana aplikacja Śledź na jednym wybranym urządzeniu pobierze je i zapisze lokalnie. Zaraz potem usuwa je z serwera. Historia jest dostępna wyłącznie w tej przeglądarce — inne urządzenia i logowania widzą tylko listę lokalizatorów.</p>
        <p class="hint">Tracker nadal wysyła każdą pozycję na serwer, który trzyma ją tylko do pobrania. Utrata urządzenia, odinstalowanie aplikacji lub wyczyszczenie danych przeglądarki oznacza utratę historii — nie ma jej kopii na serwerze.</p>
        <dl class="facts">
          <div><dt>Stan</dt><dd>{{if .Privacy}}<span class="pill ok">Włączony</span>{{else}}<span class="pill offline">Wyłączony</span>{{end}}</dd></div>
          {{if .Privacy}}<div><dt>Powiązane urządzenie</dt><dd>{{.Settings.PrivacyClientLabel}}{{if .BoundAt}}, od <time data-local datetime="{{.BoundAt}}">{{.BoundAt}}</time>{{end}}</dd></div>{{end}}
          <div><dt>Pozycje na serwerze</dt><dd id="server-positions">{{.ServerPositions}}</dd></div>
        </dl>
        <div class="local-status" id="privacy-local" hidden></div>
        <p class="hint" id="privacy-install" hidden>Aby włączyć tryb prywatny na tym urządzeniu, zainstaluj Śledzia jako aplikację (w menu przeglądarki: „Zainstaluj aplikację” lub „Dodaj do ekranu początkowego”) i otwórz te ustawienia w aplikacji.</p>
        <div class="actions">
          <button class="btn" id="privacy-enable" type="button" hidden>Włącz tryb prywatny na tym urządzeniu</button>
          <button class="btn secondary" id="privacy-disable" type="button" hidden>Wyłącz tryb prywatny</button>
          <button class="btn danger" id="privacy-wipe" type="button" hidden>Usuń dane z tej przeglądarki</button>
        </div>
        <p class="notice" id="privacy-message" role="status" hidden></p>
        <noscript><p class="notice alert">Tryb prywatny wymaga włączonego JavaScriptu.</p></noscript>
      </section>
      <section class="card" id="retention" aria-labelledby="retention-title">
        <h2 id="retention-title">Przechowywanie danych</h2>
        <form method="post" action="/ustawienia">
          <label>Na serwerze (dni)<input type="number" name="server_retention_days" inputmode="numeric" min="0" max="{{.MaxRetentionDays}}" value="{{.Settings.ServerRetentionDays}}" required></label>
          <p class="hint">Pozycje starsze niż podana liczba dni są usuwane z serwera co godzinę i od razu po zapisaniu. 0 oznacza bez limitu. W trybie prywatnym dotyczy pozycji, których powiązane urządzenie jeszcze nie pobrało.</p>
          <label>W przeglądarce, w trybie prywatnym (dni)<input type="number" name="browser_retention_days" inputmode="numeric" min="0" max="{{.MaxRetentionDays}}" value="{{.Settings.BrowserRetentionDays}}" required></label>
          <p class="hint">Powiązane urządzenie usuwa starsze pozycje ze swojej pamięci przy każdej synchronizacji. 0 oznacza bez limitu.</p>
          <button class="btn" type="submit">Zapisz</button>
        </form>
      </section>
    </div>
  </main>
</div>
<script src="/assets/privacy.js?v=1"></script>
<script src="/assets/settings.js?v=1"></script>
</body></html>{{end}}
`
