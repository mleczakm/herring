package httpapi

// pages holds every server-rendered page. Assets (fonts, Leaflet, dashboard
// JS) are self-hosted (see assets/ and dashboardJS) instead of pulled from a
// CDN at runtime, so the strict CSP in securityHeaders never needs to trust a
// third party for script execution — only map tile images are cross-origin.
const pages = `
{{define "head"}}<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/assets/leaflet.css">{{template "styles"}}{{end}}

{{define "styles"}}<style>
@font-face{font-family:'Space Grotesk';font-style:normal;font-weight:500 700;font-display:swap;src:url(/assets/space-grotesk-latin.woff2) format('woff2');unicode-range:U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+2000-206F,U+20AC,U+2122;}
@font-face{font-family:'Space Grotesk';font-style:normal;font-weight:500 700;font-display:swap;src:url(/assets/space-grotesk-latin-ext.woff2) format('woff2');unicode-range:U+0100-024F,U+0304,U+0308,U+0329,U+1E00-1EFF;}
:root{
  --blue:#0284C7;--blue-dark:#0369A1;--accent:#F97316;
  --bg:#F3F4F6;--surface:#ffffff;
  --ink:#111827;--ink-soft:#4B5563;--ink-faint:#6B7280;--border:#E5E7EB;
  --ok:#10B981;--alert:#EF4444;--warn:#F59E0B;--offline:#6B7280;
  --radius:12px;--radius-lg:16px;
  --shadow-glass:0 8px 32px rgba(31,38,135,.12);
  --font-sans:Inter,ui-sans-serif,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;
  --font-title:'Space Grotesk',var(--font-sans);
}
*{box-sizing:border-box}
html,body{height:100%}
body{margin:0;font:15px/1.5 var(--font-sans);color:var(--ink);background:var(--bg)}
h1,h2,h3{font-family:var(--font-title);margin:0}
button{font:inherit}
a{color:inherit}
.icon{width:1.25em;height:1.25em;flex-shrink:0;stroke:currentColor;fill:none;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round}
.glass{background:rgba(255,255,255,.86);backdrop-filter:blur(12px);-webkit-backdrop-filter:blur(12px);border:1px solid rgba(255,255,255,.5);box-shadow:var(--shadow-glass)}
.btn{display:inline-flex;align-items:center;justify-content:center;gap:.4rem;border:0;border-radius:.6rem;padding:.65rem 1.1rem;font-weight:600;cursor:pointer;background:var(--blue);color:#fff;transition:background .15s}
.btn:hover{background:var(--blue-dark)}
.btn.secondary{background:var(--border);color:var(--ink)}
.btn.secondary:hover{background:#d7dbe0}
.btn-icon{display:inline-flex;align-items:center;justify-content:center;width:2.25rem;height:2.25rem;border-radius:50%;border:0;background:transparent;color:var(--ink-faint);cursor:pointer;transition:background .15s,color .15s}
.btn-icon:hover{background:var(--border);color:var(--ink)}
input,select{box-sizing:border-box;width:100%;padding:.65rem .8rem;border:1px solid var(--border);border-radius:.55rem;font:inherit;background:#fff}
input:focus,select:focus{outline:2px solid var(--blue);outline-offset:1px}
label{display:block;font-size:.85rem;font-weight:600;color:var(--ink-soft);margin-top:.9rem}
label input,label select{margin-top:.3rem}
.error{color:var(--alert);font-size:.9rem;margin:.6rem 0 0}
.pill{display:inline-flex;align-items:center;gap:.3rem;font-size:.75rem;font-weight:600;padding:.15rem .55rem;border-radius:999px}
.pill.ok{background:#d1fae5;color:#047857}
.pill.warn{background:#fef3c7;color:#b45309}
.pill.alert{background:#fee2e2;color:#b91c1c}
.pill.offline{background:#e5e7eb;color:#4b5563}
.pill.info{background:#dbeafe;color:#1d4ed8}

/* Auth pages */
.auth-shell{min-height:100%;display:flex;align-items:center;justify-content:center;padding:2rem;background:radial-gradient(circle at top,#e0f2fe,var(--bg) 60%)}
.auth-card{width:min(26rem,100%);background:var(--surface);border-radius:var(--radius-lg);box-shadow:var(--shadow-glass);padding:2.25rem}
.auth-brand{display:flex;align-items:center;gap:.7rem;margin-bottom:1.4rem}
.auth-brand svg{width:2.75rem;height:1.4rem}
.auth-brand h1{font-size:1.15rem;color:var(--ink)}
.auth-brand p{margin:0;font-size:.75rem;color:var(--blue);font-weight:600}
.auth-card form button{width:100%;margin-top:1.4rem}
.auth-foot{margin-top:1.2rem;font-size:.85rem;color:var(--ink-faint);text-align:center}
.auth-foot a{color:var(--blue);font-weight:600;text-decoration:none}

/* Dashboard shell */
.app{display:flex;height:100%;overflow:hidden}
.sidebar{width:17rem;flex-shrink:0;background:var(--surface);border-right:1px solid var(--border);display:flex;flex-direction:column;z-index:30;transition:transform .25s ease}
.sidebar-brand{display:flex;align-items:center;gap:.7rem;padding:1.3rem 1.4rem;border-bottom:1px solid var(--border);height:4.75rem}
.sidebar-brand svg{width:2.75rem;height:1.4rem;flex-shrink:0}
.sidebar-brand h1{font-size:1.05rem;line-height:1.2}
.sidebar-brand p{margin:0;font-size:.72rem;color:var(--blue);font-weight:600}
.sidebar-nav{flex:1;overflow-y:auto;padding:1rem .7rem}
.sidebar-nav ul{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:.2rem}
.nav-link{display:flex;align-items:center;gap:.75rem;padding:.65rem .8rem;border-radius:.6rem;color:var(--ink-soft);text-decoration:none;font-weight:500;position:relative}
.nav-link:hover{background:#f3f4f6;color:var(--ink)}
.nav-link.active{background:rgba(2,132,199,.1);color:var(--blue)}
.nav-link .soon{margin-left:auto;font-size:.65rem;font-weight:700;color:var(--ink-faint);background:var(--border);padding:.1rem .4rem;border-radius:999px}
.sidebar-foot{border-top:1px solid var(--border);padding:.9rem .7rem}
.sidebar-backdrop{display:none}

.content{flex:1;position:relative;display:flex;flex-direction:column;min-width:0}
.topbar{position:absolute;top:0;left:0;right:0;z-index:20;display:flex;justify-content:space-between;align-items:flex-start;padding:1rem;pointer-events:none}
.topbar>*{pointer-events:auto}
.menu-toggle{display:none}
.user-menu{position:relative}
.user-pill{display:flex;align-items:center;gap:.6rem;border-radius:999px;padding:.4rem .9rem .4rem .4rem;cursor:pointer;border:0}
.user-avatar{width:2.1rem;height:2.1rem;border-radius:50%;background:rgba(2,132,199,.15);color:var(--blue);display:flex;align-items:center;justify-content:center}
.user-pill span{font-weight:600;font-size:.9rem}
.user-dropdown{position:absolute;top:calc(100% + .5rem);right:0;min-width:11rem;background:var(--surface);border-radius:.7rem;box-shadow:var(--shadow-glass);border:1px solid var(--border);padding:.4rem;display:none}
.user-dropdown.open{display:block}
.user-dropdown form{margin:0}
.user-dropdown button{width:100%;text-align:left;background:none;border:0;padding:.55rem .7rem;border-radius:.5rem;color:var(--ink-soft);cursor:pointer;display:flex;gap:.5rem;align-items:center}
.user-dropdown button:hover{background:var(--bg);color:var(--ink)}

#map{position:absolute;inset:0}
.leaflet-popup-content-wrapper{border-radius:12px;box-shadow:0 10px 25px -5px rgba(0,0,0,.15);padding:0;overflow:hidden}
.leaflet-popup-content{margin:0;font-family:var(--font-sans)}
.leaflet-popup-tip-container{margin-top:-1px}
.popup{min-width:14rem}
.popup-head{padding:.7rem .9rem;display:flex;justify-content:space-between;align-items:center;gap:.5rem;border-bottom:1px solid var(--border)}
.popup-head strong{font-family:var(--font-title);font-size:.95rem}
.popup-body{padding:.8rem .9rem;font-size:.85rem;color:var(--ink-soft);display:flex;flex-direction:column;gap:.4rem}
.popup-row{display:flex;justify-content:space-between;align-items:center;gap:1rem}

.marker{width:2.5rem;height:2.5rem;display:flex;align-items:center;justify-content:center;position:relative}
.marker-pin{width:2.5rem;height:2.5rem;border-radius:50% 50% 50% 0;transform:rotate(-45deg);display:flex;align-items:center;justify-content:center;box-shadow:0 3px 6px rgba(0,0,0,.25);border:2px solid #fff}
.marker-pin .icon{transform:rotate(45deg);stroke:#fff;width:1.15rem;height:1.15rem}
.marker.moving .marker-pin{animation:none}
.marker.moving .marker-pin::after{content:'';position:absolute;inset:0;border-radius:50% 50% 50% 0;background:inherit;z-index:-1;animation:pulse 2s infinite}
@keyframes pulse{0%{opacity:.55}100%{transform:scale(2.1);opacity:0}}

.device-panel{position:absolute;top:5.4rem;left:1rem;bottom:1.2rem;width:21rem;border-radius:var(--radius-lg);z-index:20;display:flex;flex-direction:column;overflow:hidden;transition:transform .25s ease}
.device-panel-head{padding:1rem 1rem .8rem;border-bottom:1px solid rgba(229,231,235,.7)}
.device-panel-head h2{font-size:1.05rem;display:flex;align-items:center;gap:.5rem;color:var(--ink)}
.device-panel-head h2 .icon{color:var(--blue)}
.search{position:relative;margin-top:.7rem}
.search .icon{position:absolute;left:.7rem;top:50%;transform:translateY(-50%);color:var(--ink-faint)}
.search input{padding-left:2.1rem;background:rgba(255,255,255,.7)}
.device-list{flex:1;overflow-y:auto;padding:.5rem}
.device-item{display:flex;align-items:center;gap:.7rem;padding:.7rem;border-radius:.8rem;cursor:pointer;border:1px solid transparent}
.device-item:hover{background:rgba(255,255,255,.6);border-color:var(--border)}
.device-avatar{width:2.5rem;height:2.5rem;border-radius:50%;flex-shrink:0;display:flex;align-items:center;justify-content:center;color:#fff;box-shadow:0 1px 3px rgba(0,0,0,.2)}
.device-avatar .icon{width:1.2rem;height:1.2rem;stroke:#fff}
.device-info{flex:1;min-width:0}
.device-info h3{font-size:.9rem;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.device-meta{display:flex;align-items:center;gap:.4rem;font-size:.78rem;color:var(--ink-faint);margin-top:.15rem}
.device-meta .icon{width:1rem;height:1rem}
.empty-state{padding:1.5rem 1.2rem;text-align:center;color:var(--ink-faint)}
.empty-state svg{width:4.5rem;margin:0 auto .8rem;display:block}
.empty-state p{margin:.3rem 0 0;font-size:.85rem}
.device-panel-foot{padding:.8rem;border-top:1px solid rgba(229,231,235,.7)}
.device-panel-foot .btn{width:100%}

.add-device-panel{position:absolute;top:5.4rem;right:1rem;width:22rem;max-width:calc(100vw - 2rem);border-radius:var(--radius-lg);z-index:25;padding:1.2rem;display:none}
.add-device-panel.open{display:block}
.add-device-panel-head{display:flex;justify-content:space-between;align-items:center}
.add-device-panel-head h2{font-size:1.05rem}
.add-device-panel .hint{font-size:.82rem;color:var(--ink-faint);margin-top:.3rem}

.toast{position:absolute;bottom:1.2rem;right:1.2rem;z-index:40;width:20rem;max-width:calc(100vw - 2rem);border-radius:var(--radius-lg);padding:1rem;border-left:4px solid var(--blue);display:none;animation:slideIn .4s ease}
.toast.show{display:flex;gap:.7rem;align-items:flex-start}
@keyframes slideIn{from{transform:translateX(20%);opacity:0}to{transform:translateX(0);opacity:1}}
.toast-avatar{width:2.4rem;height:2.4rem;border-radius:50%;background:var(--bg);border:1px solid var(--border);display:flex;align-items:center;justify-content:center;flex-shrink:0}
.toast-avatar svg{width:1.9rem}
.toast h4{font-size:.85rem;color:var(--ink)}
.toast p{margin:.25rem 0 0;font-size:.8rem;color:var(--ink-soft);line-height:1.4}
.toast .btn-icon{margin-left:auto;width:1.8rem;height:1.8rem}

@media (max-width:768px){
  .sidebar{position:fixed;inset:0 auto 0 0;transform:translateX(-100%)}
  .sidebar.open{transform:translateX(0)}
  .sidebar-backdrop{display:none;position:fixed;inset:0;background:rgba(17,24,39,.4);z-index:29}
  .sidebar-backdrop.open{display:block}
  .menu-toggle{display:inline-flex}
  .device-panel{left:.6rem;right:.6rem;width:auto;bottom:auto;top:5.4rem;max-height:45vh}
  .add-device-panel{left:.6rem;right:.6rem;width:auto}
  .toast{left:.6rem;right:.6rem;width:auto}
}
</style>{{end}}

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

{{define "brand-mark"}}<svg viewBox="0 0 100 50" xmlns="http://www.w3.org/2000/svg"><path d="M45,25 L35,5 L55,25 Z" fill="#9CA3AF"/><path d="M45,25 L35,45 L55,25 Z" fill="#9CA3AF"/><path d="M5,25 C25,0 75,0 95,25 C75,50 25,50 5,25 Z" fill="#E5E7EB" stroke="#6B7280" stroke-width="2"/><path d="M5,25 L-5,10 L-5,40 Z" fill="#E5E7EB" stroke="#6B7280" stroke-width="2"/><rect x="68" y="16" width="14" height="7" rx="2" fill="#111827"/><rect x="52" y="16" width="14" height="7" rx="2" fill="#111827"/><line x1="66" y1="19" x2="68" y2="19" stroke="#111827" stroke-width="2"/><circle cx="50" cy="23" r="2.5" fill="#D1D5DB" stroke="#4B5563" stroke-width="1"/><circle cx="50" cy="23" r="1" fill="#EF4444"/></svg>{{end}}

{{define "setup"}}<!doctype html><html lang="pl"><head><title>Herring — konfiguracja</title>{{template "head"}}</head><body><div class="auth-shell"><main class="auth-card"><div class="auth-brand">{{template "brand-mark"}}<div><h1>Witaj w Herring</h1><p>Agent Śledź melduje gotowość</p></div></div>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post">{{if .TokenRequired}}<label>Token instalacji<input name="setup_token" type="password" required></label>{{end}}<label>Nazwa<input name="display_name" value="{{.DisplayName}}" required></label><label>Email<input name="email" type="email" value="{{.Email}}" required></label><label>Hasło<input name="password" type="password" minlength="12" required></label><label>Powtórz hasło<input name="password_confirmation" type="password" minlength="12" required></label><button class="btn">Utwórz administratora</button></form></main></div></body></html>{{end}}

{{define "login"}}<!doctype html><html lang="pl"><head><title>Herring — logowanie</title>{{template "head"}}</head><body><div class="auth-shell"><main class="auth-card"><div class="auth-brand">{{template "brand-mark"}}<div><h1>Herring</h1><p>śledź.mleczki.pl</p></div></div>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post"><label>Email<input name="email" type="email" required></label><label>Hasło<input name="password" type="password" required></label><button class="btn">Zaloguj</button></form></main></div></body></html>{{end}}

{{define "home"}}<!doctype html><html lang="pl"><head><title>Herring — Mapa Live</title>{{template "head"}}</head><body>
<div class="app">
  <div class="sidebar-backdrop" id="sidebar-backdrop"></div>
  <aside class="sidebar" id="sidebar">
    <div class="sidebar-brand">{{template "brand-mark"}}<div><h1>śledź.mleczki.pl</h1><p>System Śledzenia</p></div></div>
    <nav class="sidebar-nav"><ul>
      <li><a class="nav-link active" href="/">{{template "icon-map"}}<span>Mapa Live</span></a></li>
      <li><a class="nav-link" href="/">{{template "icon-crosshair"}}<span>Urządzenia</span></a></li>
      <li><a class="nav-link" href="/">{{template "icon-path"}}<span>Historia Tras</span><span class="soon">wkrótce</span></a></li>
      <li><a class="nav-link" href="/">{{template "icon-geofence"}}<span>Strefy</span><span class="soon">wkrótce</span></a></li>
      <li><a class="nav-link" href="/">{{template "icon-bell"}}<span>Powiadomienia</span><span class="soon">wkrótce</span></a></li>
    </ul></nav>
    <div class="sidebar-foot"><a class="nav-link" href="/">{{template "icon-gear"}}<span>Ustawienia</span><span class="soon">wkrótce</span></a></div>
  </aside>
  <main class="content">
    <header class="topbar">
      <button class="btn-icon menu-toggle glass" id="menu-toggle" type="button">{{template "icon-list"}}</button>
      <div class="user-menu">
        <button class="user-pill glass" id="user-menu-toggle" type="button"><span class="user-avatar">{{template "icon-user"}}</span><span>{{.User.DisplayName}}</span>{{template "icon-caret"}}</button>
        <div class="user-dropdown" id="user-dropdown"><form method="post" action="/logout"><button type="submit">{{template "icon-logout"}}Wyloguj</button></form></div>
      </div>
    </header>
    <div id="map"></div>
    <section class="device-panel glass" id="device-panel">
      <div class="device-panel-head">
        <h2>{{template "icon-crosshair"}}Moje Cele</h2>
        <div class="search">{{template "icon-search"}}<input type="text" id="device-search" placeholder="Szukaj urządzenia…"></div>
      </div>
      <div class="device-list" id="device-list" data-empty-html="{{if not .Devices}}1{{end}}">
        {{if not .Devices}}<div class="empty-state"><svg viewBox="0 0 100 50" xmlns="http://www.w3.org/2000/svg"><path d="M45,25 L35,5 L55,25 Z" fill="#D1D5DB"/><path d="M45,25 L35,45 L55,25 Z" fill="#D1D5DB"/><path d="M5,25 C25,0 75,0 95,25 C75,50 25,50 5,25 Z" fill="#F3F4F6" stroke="#9CA3AF" stroke-width="2"/><path d="M5,25 L-5,10 L-5,40 Z" fill="#F3F4F6" stroke="#9CA3AF" stroke-width="2"/><rect x="68" y="16" width="14" height="7" rx="2" fill="#374151"/><rect x="52" y="16" width="14" height="7" rx="2" fill="#374151"/></svg><strong>Brak celów na radarze</strong><p>Agent Śledź czeka na rozkazy. Dodaj pierwszy tracker, żeby zacząć misję.</p></div>
        {{else}}{{range .Devices}}<div class="device-item" data-id="{{.ID}}"><span class="device-avatar" style="background:var(--offline)">{{template "icon-crosshair"}}</span><div class="device-info"><h3>{{if .Name}}{{.Name}}{{else}}{{.PhoneNumber}}{{end}}</h3><div class="device-meta"><span data-role="summary">{{if eq .ConfigStatus "configured"}}Oczekiwanie na sygnał…{{else if eq .ConfigStatus "failed"}}⚠ Konfiguracja nieudana{{else}}⏳ Konfigurowanie…{{end}}</span></div></div></div>{{end}}
        {{end}}
      </div>
      <div class="device-panel-foot"><button class="btn" id="open-add-device" type="button">{{template "icon-plus"}}Dodaj nowy tracker</button></div>
    </section>
    <section class="add-device-panel glass" id="add-device-panel">
      <div class="add-device-panel-head"><h2>Dodaj tracker</h2><button class="btn-icon" id="close-add-device" type="button">{{template "icon-x"}}</button></div>
      {{if .Ready}}<form method="post" action="/devices"><label>Wariant<select name="model"><option value="st901-2g">ST-901 2G</option><option value="st901-4g">ST-901 4G</option></select></label><label>Numer SIM trackera<input name="phone" type="tel" placeholder="+48…" required></label><label>Nazwa (opcjonalnie)<input name="name"></label><button class="btn" type="submit">Dodaj i skonfiguruj przez SMS</button></form>{{else}}<p class="hint">Administrator serwera musi najpierw skonfigurować integrację Sendly.</p>{{end}}
    </section>
    <div class="toast glass" id="agent-toast">
      <span class="toast-avatar">{{template "brand-mark"}}</span>
      <div><h4>Agent Śledź melduje</h4><p id="toast-message"></p></div>
      <button class="btn-icon" id="toast-close" type="button">{{template "icon-x"}}</button>
    </div>
  </main>
</div>
<script src="/assets/leaflet.js"></script>
<script src="/assets/dashboard.js"></script>
</body></html>{{end}}
`
