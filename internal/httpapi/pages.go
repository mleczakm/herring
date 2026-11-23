package httpapi

import _ "embed"

//go:embed public.html
var publicPages string

// pages holds the dashboard and shared template components. Assets (fonts, Leaflet, dashboard
// JS) are self-hosted (see assets/ and dashboardJS) instead of pulled from a
// CDN at runtime, so the strict CSP in securityHeaders never needs to trust a
// third party for script execution — only map tile images are cross-origin.
const pages = `
{{define "head"}}<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#0e3332"><link rel="icon" href="/assets/herring.svg?v=2" type="image/svg+xml"><link rel="manifest" href="/manifest.webmanifest"><link rel="apple-touch-icon" href="/assets/icon-180.png?v=2"><link rel="stylesheet" href="/assets/herring.css?v=4">{{end}}



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

{{define "brand-mark"}}<img class="brand-mark" src="/assets/herring.svg?v=2" width="44" height="44" alt="">{{end}}
{{define "wordmark"}}{{template "brand-mark"}}<span class="wordmark">Śledź</span>{{end}}

{{define "sidebar"}}<aside class="sidebar" id="sidebar">
    <a class="sidebar-brand" href="/o-projekcie">{{template "brand-mark"}}<div><span class="wordmark">Śledź</span><small>Panel lokalizacji</small></div></a>
    <nav class="sidebar-nav"><ul>
      <li><a class="nav-link{{if eq .Active "map"}} active{{end}}" href="/">{{template "icon-map"}}<span>Mapa na żywo</span></a></li>
      <li><a class="nav-link{{if eq .Active "devices"}} active{{end}}" href="/urzadzenia">{{template "icon-crosshair"}}<span>Urządzenia</span></a></li>
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
      <div class="device-panel-foot"><button class="btn" id="open-add-device" type="button">{{template "icon-plus"}}Dodaj lokalizator</button>{{if .Devices}}<a class="manage-link" href="/urzadzenia">Zmień nazwę, ponów konfigurację lub usuń</a>{{end}}</div>
    </section>
    <section class="add-device-panel glass" id="add-device-panel">
      <div class="add-device-panel-head"><h2>Dodaj lokalizator</h2><button class="btn-icon" id="close-add-device" aria-label="Zamknij formularz" type="button">{{template "icon-x"}}</button></div>
      {{if .Ready}}<form method="post" action="/devices"><label>Wariant<select name="model"><option value="st901-2g">ST-901 2G</option><option value="st901-4g">ST-901 4G</option></select></label><label>Numer telefonu karty SIM<input name="phone" type="tel" placeholder="+48…" required></label><label>Nazwa (opcjonalnie)<input name="name" maxlength="80"></label><label>ID lokalizatora (opcjonalnie)<input name="tracker_id" id="add-tracker-id" inputmode="numeric" autocomplete="off" pattern="[0-9 ]{5,30}" list="unknown-trackers"></label><p class="hint">Znajdziesz je na naklejce lokalizatora, w polu ID. Bez niego Śledź nie przyjmie pozycji, dopóki nie wybierzesz go w „Urządzenia”.</p>{{template "unknown-picker" .Unknown}}<button class="btn" type="submit">Dodaj i skonfiguruj przez SMS</button></form>{{else}}<p class="hint">Aby dodać lokalizator, poproś administratora o włączenie wysyłki SMS.</p>{{end}}
    </section>
    <div class="toast glass" role="status" id="agent-toast">
      <span class="toast-avatar">{{template "brand-mark"}}</span>
      <div><h4>Agent Śledź melduje</h4><p id="toast-message"></p><button class="btn btn-small" id="toast-action" type="button" hidden></button></div>
      <button class="btn-icon" id="toast-close" aria-label="Zamknij komunikat" type="button">{{template "icon-x"}}</button>
    </div>
  </main>
</div>
<script src="/assets/leaflet.js"></script>
<script src="/assets/privacy.js?v=2"></script>
<script src="/assets/dashboard.js?v=5"></script>
</body></html>{{end}}

{{define "unknown-picker"}}{{if .}}<div class="unknown-trackers"><p class="hint">Te lokalizatory próbowały się ostatnio połączyć, ale Śledź ich nie zna. Wybierz swój tylko wtedy, gdy ID zgadza się z naklejką:</p><ul>{{range .}}<li><button type="button" class="chip" data-tracker-id="{{.ID}}">{{.ID}}</button><span>{{.LastSeen}}, {{.Attempts}}</span></li>{{end}}</ul></div>{{end}}<datalist id="unknown-trackers">{{range .}}<option value="{{.ID}}">{{.LastSeen}}</option>{{end}}</datalist>{{end}}

{{define "devices"}}<!doctype html><html lang="pl"><head><title>Urządzenia · Śledź</title>{{template "head"}}</head><body{{if .Privacy}} data-privacy="local"{{end}}>
<div class="app">
  <div class="sidebar-backdrop" id="sidebar-backdrop"></div>
  {{template "sidebar" .}}
  <main class="content page">
    <header class="page-head">
      <button class="btn-icon menu-toggle" id="menu-toggle" aria-controls="sidebar" aria-expanded="false" aria-label="Menu nawigacji" type="button">{{template "icon-list"}}</button>
      <h1>{{template "icon-crosshair"}} Urządzenia</h1>
    </header>
    <div class="page-body">
      {{if .Notice}}<p class="notice" role="status">{{.Notice}}</p>{{end}}
      {{if .Error}}<p class="notice alert" role="alert">{{.Error}}</p>{{end}}
      {{if .Unknown}}<section class="card" id="nieznane" aria-labelledby="unknown-title">
        <h2 id="unknown-title">Nieznane lokalizatory</h2>
        <p>Te lokalizatory próbowały wysłać pozycję w ostatnim tygodniu, ale Śledź ich nie zna, więc odrzucił ich dane. Jeśli któryś to twój, wpisz jego ID przy urządzeniu poniżej.</p>
        <dl class="facts">{{range .Unknown}}<div><dt><code>{{.ID}}</code></dt><dd>{{.LastSeen}}, {{.Attempts}}</dd></div>{{end}}</dl>
        <p class="hint">Każdy może próbować połączyć się ze Śledziem, więc na tej liście może być cudzy lub przypadkowy numer. Wybieraj tylko ID, które widzisz na swoim lokalizatorze.</p>
      </section>{{end}}
      {{if not .Devices}}<section class="card"><h2>Brak urządzeń</h2><p>Nie masz jeszcze żadnego lokalizatora.</p><a class="btn" href="/#dodaj">Dodaj lokalizator</a></section>{{end}}
      {{range .Devices}}<section class="card device-card" id="urzadzenie-{{.ID}}" aria-labelledby="device-{{.ID}}-title">
        <h2 id="device-{{.ID}}-title">{{.Label}}</h2>
        <dl class="facts">
          <div><dt>Wariant</dt><dd>{{.ModelLabel}}</dd></div>
          <div><dt>Numer karty SIM</dt><dd>+{{.PhoneNumber}}</dd></div>
          <div><dt>ID lokalizatora</dt><dd>{{if .TrackerID}}<code>{{.TrackerID}}</code>{{else}}<span class="pill warn">Nieznane</span>{{end}}</dd></div>
          <div><dt>Konfiguracja</dt><dd><span class="pill {{.StatusClass}}">{{.StatusLabel}}</span></dd></div>
        </dl>
        {{if .ConfigDetail}}<p class="hint">{{.ConfigDetail}}</p>{{end}}
        <form method="post" action="/devices/{{.ID}}">
          <h3>Nazwa i ID</h3>
          <label>Nazwa<input name="name" value="{{.Name}}" maxlength="80" placeholder="+{{.PhoneNumber}}"></label>
          <label>ID lokalizatora<input name="tracker_id" value="{{.TrackerID}}" inputmode="numeric" autocomplete="off" pattern="[0-9 ]{5,30}" list="unknown-trackers"></label>
          {{if .TrackerID}}<p class="hint">Zmiana ID usuwa pozycje zapisane pod dotychczasowym ID.</p>{{else}}<p class="hint">Wpisz ID z naklejki lokalizatora, aby Śledź przyjmował jego pozycje.</p>{{end}}
          <button class="btn" type="submit">Zapisz</button>
        </form>
        <form method="post" action="/devices/{{.ID}}/configure">
          <h3>Konfiguracja przez SMS</h3>
          <p class="hint">Wysyła ponownie wszystkie SMS-y z ustawieniami połączenia. Przydaje się, gdy konfiguracja się nie udała albo lokalizator był wyłączony. Każdy SMS kosztuje.</p>
          <button class="btn secondary" type="submit"{{if not $.Ready}} disabled{{end}}>Wyślij ustawienia ponownie</button>
        </form>
        <form method="post" action="/devices/{{.ID}}/delete">
          <h3>Usuwanie</h3>
          <label class="check"><input type="checkbox" name="confirm" value="1" required><span>Rozumiem, że usunę też wszystkie pozycje tego urządzenia zapisane na serwerze. Tego nie da się cofnąć.</span></label>
          <button class="btn danger" type="submit">Usuń urządzenie</button>
        </form>
      </section>{{end}}
      {{if .Devices}}<p><a class="btn secondary" href="/#dodaj">{{template "icon-plus"}}Dodaj lokalizator</a></p>{{end}}
      <datalist id="unknown-trackers">{{range .Unknown}}<option value="{{.ID}}">{{.LastSeen}}</option>{{end}}</datalist>
    </div>
  </main>
</div>
<script src="/assets/devices.js?v=1"></script>
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
      <section class="card" id="sms" aria-labelledby="sms-title">
        <h2 id="sms-title">SMS-y i stan konta</h2>
        <dl class="facts">
          <div><dt>SMS-y wysłane do lokalizatorów w ostatnich 30 dniach</dt><dd>{{.SMSLast30Days}}</dd></div>
          <div><dt>Wysyłanie SMS-ów</dt><dd>{{if .SMSReady}}<span class="pill ok">Gotowe</span>{{else}}<span class="pill offline">Nieskonfigurowane</span>{{end}}</dd></div>
        </dl>
        <p class="hint">Każde zapytanie SMS-em o pozycję to jeden SMS ze Śledzia i jedna odpowiedź wysłana z karty SIM lokalizatora, płatna u jej operatora.</p>
        <p class="hint">Śledź nie może sprawdzić stanu konta: lokalizator nie ma do tego polecenia, a usługa wysyłająca SMS-y nie udostępnia salda. Sprawdzaj je u operatora karty SIM i w panelu usługi SMS. Gdy lokalizator nagle milknie, jedną z przyczyn bywa brak środków na karcie — włącz powiadomienie o braku sygnału w „Miejsca i powiadomienia”.</p>
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
<script src="/assets/privacy.js?v=2"></script>
<script src="/assets/settings.js?v=1"></script>
</body></html>{{end}}
`
