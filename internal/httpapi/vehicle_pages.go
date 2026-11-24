package httpapi

const vehiclePages = `
{{define "vehicle-head"}}{{template "head"}}<link rel="stylesheet" href="/assets/leaflet.css">{{end}}
{{define "vehicle-menu"}}<button class="btn-icon menu-toggle" id="menu-toggle" aria-controls="sidebar" aria-expanded="false" aria-label="Menu nawigacji" type="button">{{template "icon-list"}}</button>{{end}}
{{define "vehicle-scripts"}}<script src="/assets/leaflet.js"></script><script src="/assets/vehicle-maps.js?v=1"></script>{{end}}
{{define "vehicle-id"}}<input type="hidden" name="id" value="{{.Device.ID}}">{{end}}

{{define "vehicles"}}<!doctype html><html lang="pl"><head><title>Pojazdy · Śledź</title>{{template "vehicle-head"}}</head><body>
<div class="app"><div class="sidebar-backdrop" id="sidebar-backdrop"></div>{{template "sidebar" .}}
<main class="content page vehicle-page"><header class="page-head">{{template "vehicle-menu"}}<h1>Pojazdy</h1></header><div class="page-body">
{{if .Issue}}<p class="notice alert" role="alert">{{.Issue}}</p>{{end}}
{{if not .Device.ID}}<p>Dodaj lokalizator na mapie, aby skonfigurować pojazd.</p><a href="/">Przejdź do mapy</a>{{else}}
<section class="card"><nav aria-label="Pojazdy">{{range .Devices}}<a class="btn btn-small" href="/pojazdy/{{.ID}}" {{if eq .ID $.Device.ID}}aria-current="page"{{end}}>{{if .Name}}{{.Name}}{{else}}{{.PhoneNumber}}{{end}}</a> {{end}}</nav>
<h2>{{if .Device.Name}}{{.Device.Name}}{{else}}{{.Device.PhoneNumber}}{{end}}</h2>
<p><a href="/trasy?id={{.Device.ID}}">Historia przejazdów i statystyki</a></p>
<form method="post" action="/pojazdy/{{.Device.ID}}">
<label>Typ pojazdu <select name="kind">{{range .Types}}<option value="{{.Value}}" {{if eq .Value $.Profile.Kind}}selected{{end}}>{{.Label}}</option>{{end}}</select></label>
<label>Ikona na mapie <select name="icon">{{range .Icons}}<option value="{{.Value}}" {{if eq .Value $.Profile.Icon}}selected{{end}}>{{.Symbol}} {{.Label}}</option>{{end}}</select></label>
<button class="btn btn-small" name="action" value="profile">Zapisz wygląd</button>
</form></section>

<section class="card"><h2>Serwis</h2>
<p>Przebieg rośnie na podstawie nowych pozycji GPS. Możesz wpisać aktualny odczyt licznika. W trybie prywatnym przebieg nie narasta automatycznie.</p>
<form method="post" action="/pojazdy/{{.Device.ID}}">
<label>Przebieg (km) <input name="odometer" inputmode="decimal" required value="{{printf "%.2f" (km .Profile.OdometerM)}}"></label>
<button class="btn btn-small" name="action" value="odometer">Ustaw przebieg</button>
</form>
<p>Powiadomienie pojawi się po osiągnięciu daty lub przebiegu, zależnie od tego, co nastąpi wcześniej. Włącz <a href="/powiadomienia">powiadomienia push</a>, aby je otrzymywać.</p>
<form method="post" action="/pojazdy/{{.Device.ID}}">
<label>Nazwa <input name="title" maxlength="100" placeholder="np. Wymiana oleju" required></label>
<label>Data <input type="date" name="date"></label>
<label>Próg przebiegu (km) <input name="km" inputmode="decimal"></label>
<button class="btn btn-small" name="action" value="add-reminder">Dodaj przypomnienie</button>
</form>
{{range .Reminders}}<form method="post" action="/pojazdy/{{$.Device.ID}}">
<p><strong>{{.Title}}</strong>{{if .Date}} · {{.Date}}{{end}}{{if .Meters}} · {{printf "%.2f" (km .Meters)}} km{{end}}
{{if .Notified}} · Powiadomienie wysłane{{end}}
<input type="hidden" name="reminder" value="{{.ID}}"> <button class="btn btn-small" name="action" value="delete-reminder">Usuń</button></p></form>{{end}}
</section>

<section class="card"><h2>Ochrona pojazdu</h2>
{{if .Privacy}}<p>Ochrona jest dostępna po wyłączeniu trybu prywatnego.</p>{{else}}
<p>Uzbrojenie wyznacza strefę wokół ostatniej pozycji zaparkowanego pojazdu. Alert push wymaga dwóch kolejnych odczytów wskazujących ruch lub pozycję poza strefą.</p>
{{if .Guard.Radius}}<p role="status">{{if .Guard.Triggered}}Alarm uruchomiony. Rozbrój i uzbrój ponownie po zaparkowaniu.{{else}}Ochrona uzbrojona · {{printf "%.0f" .Guard.Radius}} m{{end}}</p>
<form method="post" action="/pojazdy/{{.Device.ID}}"><button class="btn btn-small" name="action" value="disarm">Rozbrój</button></form>
{{else}}<form method="post" action="/pojazdy/{{.Device.ID}}"><label>Promień (m) <input name="radius" type="number" min="30" max="2000" value="75" required></label>
<button class="btn btn-small" name="action" value="arm">Uzbrój ochronę</button></form>{{end}}{{end}}
</section>

<section class="card"><h2>Udostępnianie lokalizacji</h2>
{{if .Privacy}}<p>Udostępnianie jest dostępne po wyłączeniu trybu prywatnego.</p>{{else}}
<p>Każdy z linkiem może zobaczyć ostatnią ważną pozycję do chwili wygaśnięcia lub odwołania.</p>
{{if .ShareURL}}<label>Link do skopiowania <input readonly id="share-link" value="{{.ShareURL}}"></label> <button class="btn btn-small" type="button" id="copy-share">Kopiuj link</button> <a href="{{.ShareURL}}">Otwórz mapę</a>{{end}}
<form method="post" action="/pojazdy/{{.Device.ID}}"><label>Ważność <select name="minutes"><option value="15">15 minut</option><option value="60">1 godzina</option><option value="240">4 godziny</option><option value="1440">24 godziny</option></select></label>
<button class="btn btn-small" name="action" value="share">Utwórz link</button></form>
{{if .Shares}}<form method="post" action="/pojazdy/{{.Device.ID}}"><p>Aktywne linki: {{.Shares}} <button class="btn btn-small" name="action" value="revoke">Odwołaj wszystkie</button></p></form>{{end}}
{{end}}</section>
{{end}}</div></main></div>{{template "vehicle-scripts"}}</body></html>{{end}}

{{define "trips"}}<!doctype html><html lang="pl"><head><title>Przejazdy · Śledź</title>{{template "vehicle-head"}}</head><body>
<div class="app"><div class="sidebar-backdrop" id="sidebar-backdrop"></div>{{template "sidebar" .}}
<main class="content page vehicle-page"><header class="page-head">{{template "vehicle-menu"}}<h1>Przejazdy i statystyki</h1></header><div class="page-body">
{{if .Issue}}<p class="notice alert" role="alert">{{.Issue}}</p>{{end}}
{{if .Device.ID}}<section class="card">
<form method="get" action="/trasy">
<label>Pojazd <select name="id">{{range .Devices}}<option value="{{.ID}}" {{if eq .ID $.Device.ID}}selected{{end}}>{{if .Name}}{{.Name}}{{else}}{{.PhoneNumber}}{{end}}</option>{{end}}</select></label>
<label>Od <input type="date" name="from" value="{{.From}}" required></label>
<label>Do <input type="date" name="to" value="{{.To}}" required></label>
<button class="btn btn-small">Pokaż</button></form>
{{if .Privacy}}<p id="private-trips-status" role="status">Odczytuję lokalną historię…</p>
<div id="private-trips" data-device="{{.Device.ID}}" data-from="{{.FromMS}}" data-until="{{.UntilMS}}"></div>{{else}}
<p>{{len .Trips}} przejazdów · {{printf "%.1f" .Km}} km · {{.Minutes}} min w ruchu</p>
{{if .Trips}}<a href="/trasy?id={{.Device.ID}}&amp;from={{.From}}&amp;to={{.To}}&amp;export=csv">Pobierz CSV</a>
<details><summary>Statystyki dzienne</summary><table><thead><tr><th>Dzień</th><th>Przejazdy</th><th>km</th><th>Minuty w ruchu</th></tr></thead>
<tbody>{{range .ByDay}}<tr><th scope="row">{{.Day}}</th><td>{{.Count}}</td><td>{{printf "%.1f" .Km}}</td><td>{{.Minutes}}</td></tr>{{end}}</tbody></table></details>
<details><summary>Statystyki miesięczne</summary><table><thead><tr><th>Miesiąc</th><th>Przejazdy</th><th>km</th><th>Minuty w ruchu</th></tr></thead>
<tbody>{{range .ByMonth}}<tr><th scope="row">{{.Day}}</th><td>{{.Count}}</td><td>{{printf "%.1f" .Km}}</td><td>{{.Minutes}}</td></tr>{{end}}</tbody></table></details>
{{if .Stops}}<details><summary>Najczęstsze postoje</summary><table><thead><tr><th>Miejsce</th><th>Postoje</th><th>Łącznie (min)</th></tr></thead>
<tbody>{{range .Stops}}<tr><th scope="row">{{.Label}}</th><td>{{.Count}}</td><td>{{.Minutes}}</td></tr>{{end}}</tbody></table></details>{{end}}
{{else}}<p>Brak wykrytych przejazdów w wybranym okresie.</p>{{end}}
{{end}}</section>
{{range .Trips}}
<section class="card"><h2>{{.Start}} – {{.End}}</h2>
<p>{{printf "%.2f" .Km}} km · {{.Minutes}} min w ruchu · maks. {{printf "%.0f" .Top}} km/h{{if .StopMinutes}} · postój {{.StopMinutes}} min{{end}}</p>
<button class="btn btn-small" type="button" data-track="/trasy?id={{$.Device.ID}}&amp;from={{$.From}}&amp;to={{$.To}}&amp;trip={{.Key}}&amp;export=points">Pokaż trasę</button>
<a href="/trasy?id={{$.Device.ID}}&amp;from={{$.From}}&amp;to={{$.To}}&amp;trip={{.Key}}&amp;export=gpx">Pobierz GPX</a>
<div data-track-map hidden></div><p data-track-status role="status" hidden></p>
<form method="post" action="/trasy/notatka">{{template "vehicle-id" $}}
<input type="hidden" name="from" value="{{$.From}}"><input type="hidden" name="to" value="{{$.To}}"><input type="hidden" name="trip" value="{{.Key}}">
<label>Notatka lub cel przejazdu <input name="note" value="{{.Note}}" maxlength="300"></label>
<button class="btn btn-small">Zapisz notatkę</button></form>
</section>{{end}}
{{else}}<p>Dodaj lokalizator na mapie, aby zobaczyć przejazdy.</p>{{end}}
<section class="card"><h2>Podsumowanie miesięczne</h2>
<p>Na początku miesiąca Śledź wysyła powiadomienie push z liczbą przejazdów, dystansem i czasem jazdy za poprzedni miesiąc. Podsumowanie korzysta z zachowanej historii wszystkich pojazdów i jest wstrzymane w trybie prywatnym.</p>
<form method="post" action="/trasy/podsumowanie"><label><input type="checkbox" name="enabled" {{if .Monthly}}checked{{end}}> Wysyłaj podsumowanie</label>
<button class="btn btn-small">Zapisz</button></form><p><a href="/powiadomienia">Ustawienia powiadomień push</a></p></section>
</div></main></div>{{if .Privacy}}<script src="/assets/privacy.js?v=4"></script><script src="/assets/private-trips.js?v=1" defer></script>{{end}}{{template "vehicle-scripts"}}</body></html>{{end}}

{{define "share-map"}}<!doctype html><html lang="pl"><head><title>Udostępniona lokalizacja · Śledź</title>{{template "vehicle-head"}}</head>
<body><main class="content page vehicle-page"><header class="page-head">{{template "brand-mark"}}<h1>Udostępniona lokalizacja</h1></header><div class="page-body">
<p id="share-status" role="status">Pobieranie pozycji…</p><div id="shared-map" style="height:65vh" hidden></div>
</div></main>{{template "vehicle-scripts"}}</body></html>{{end}}
`
