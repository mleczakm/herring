package httpapi

// settingsJS drives the privacy section of the settings page. The server
// renders the installation-wide state; this script adds what only this
// browser knows: whether it holds the binding secret and how much history it
// keeps locally.
const settingsJS = `(function(){
"use strict";

// --- Sidebar (mobile), as on the dashboard ---
var sidebar = document.getElementById("sidebar");
var backdrop = document.getElementById("sidebar-backdrop");
var menuToggle = document.getElementById("menu-toggle");
if (menuToggle) menuToggle.addEventListener("click", function(){
  sidebar.classList.toggle("open");
  backdrop.classList.toggle("open");
  menuToggle.setAttribute("aria-expanded", String(sidebar.classList.contains("open")));
});
if (backdrop) backdrop.addEventListener("click", function(){
  sidebar.classList.remove("open"); backdrop.classList.remove("open"); menuToggle.setAttribute("aria-expanded", "false");
});

var times = document.querySelectorAll("time[data-local]");
for (var i = 0; i < times.length; i++) times[i].textContent = formatDate(times[i].getAttribute("datetime"));

var P = window.HerringPrivacy;
var serverPrivacy = document.body.getAttribute("data-privacy") === "local";
var local = document.getElementById("privacy-local");
var installHint = document.getElementById("privacy-install");
var enableButton = document.getElementById("privacy-enable");
var disableButton = document.getElementById("privacy-disable");
var wipeButton = document.getElementById("privacy-wipe");
var message = document.getElementById("privacy-message");
if (!P || !local || !window.indexedDB) {
  if (message) { message.hidden = false; message.className = "notice alert"; message.textContent = "Ta przeglądarka nie obsługuje lokalnego przechowywania danych (IndexedDB)."; }
  return;
}

function formatDate(iso){ return iso ? new Date(iso).toLocaleString("pl-PL", {dateStyle: "medium", timeStyle: "short"}) : "—"; }
function plural(n){ return n === 1 ? "pozycja" : (n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20)) ? "pozycje" : "pozycji"; }
function say(text, isError){
  message.hidden = !text;
  message.className = isError ? "notice alert" : "notice";
  message.textContent = text || "";
}
function busy(on){ [enableButton, disableButton, wipeButton].forEach(function(b){ b.disabled = on; }); }
function reload(){ window.location.reload(); }

function persistedState(){
  return navigator.storage && navigator.storage.persisted ? navigator.storage.persisted() : Promise.resolve(false);
}

function render(state){
  var installed = P.installed();
  var lines = [];
  if (state.bound) {
    lines.push("<strong>To urządzenie przechowuje historię.</strong>");
    lines.push(state.stats.count + " " + plural(state.stats.count) + " w tej przeglądarce" + (state.stats.oldest ? ", najstarsza z " + formatDate(state.stats.oldest) : "") + ".");
    lines.push("Ostatnia synchronizacja: " + formatDate(state.stats.lastSync) + ".");
    lines.push(state.persisted ? "Przeglądarka nie usunie tych danych samoczynnie (pamięć trwała)." : "Pamięć nie jest oznaczona jako trwała — przeglądarka może usunąć dane przy braku miejsca.");
  } else if (state.stats.count > 0 || state.stats.hasKey) {
    lines.push("<strong>Ta przeglądarka nie jest powiązana z trybem prywatnym.</strong>");
    lines.push(state.stats.count > 0
      ? "Zostało w niej " + state.stats.count + " " + plural(state.stats.count) + " z wcześniejszego powiązania. Nie są już synchronizowane ani pokazywane na mapie."
      : "Nie przechowuje historii, ale zawiera klucz z wcześniejszego powiązania, który można usunąć.");
  } else if (serverPrivacy) {
    lines.push("Ta przeglądarka nie przechowuje historii.");
  }
  local.hidden = lines.length === 0;
  local.innerHTML = lines.map(function(line){ return "<p>" + line + "</p>"; }).join("");

  enableButton.hidden = state.bound || !installed;
  enableButton.textContent = serverPrivacy ? "Przenieś tryb prywatny na to urządzenie" : "Włącz tryb prywatny na tym urządzeniu";
  installHint.hidden = state.bound || installed;
  disableButton.hidden = !serverPrivacy;
  disableButton.dataset.bound = state.bound ? "1" : "";
  disableButton.textContent = state.bound ? "Wyłącz tryb prywatny i usuń dane z tej przeglądarki" : "Wyłącz tryb prywatny";
  wipeButton.hidden = state.bound || (state.stats.count === 0 && !state.stats.hasKey);
}

function refresh(){
  return P.stats().then(function(stats){
    if (!serverPrivacy || !stats.hasKey) return {bound: false, stats: stats};
    return P.sync().then(function(result){
      return P.stats().then(function(fresh){
        // Only "ok" proves the server accepted this browser's secret; network
        // errors leave the binding in place, so treat those as bound too.
        var rejected = result.status === "not_bound" || result.status === "privacy_disabled" || result.status === "no_key";
        // The count was rendered before this sync drained the server.
        var serverCount = document.getElementById("server-positions");
        if (result.status === "ok" && serverCount) serverCount.textContent = String(Math.max(0, parseInt(serverCount.textContent, 10) - result.received));
        if (!rejected && result.status !== "ok") say("Nie udało się połączyć z serwerem. Pokazuję dane zapisane lokalnie.", true);
        return {bound: !rejected, stats: fresh};
      });
    }, function(){
      say("Nie udało się połączyć z serwerem. Pokazuję dane zapisane lokalnie.", true);
      return {bound: true, stats: stats};
    });
  }).then(function(state){
    return persistedState().then(function(persisted){ state.persisted = persisted; render(state); });
  }).catch(function(error){ say("Nie udało się odczytać danych lokalnych: " + error.message, true); });
}

enableButton.addEventListener("click", function(){
  var text = serverPrivacy
    ? "Przenieść tryb prywatny na to urządzenie?\n\nHistoria zapisana na poprzednim urządzeniu zostanie tylko tam. To urządzenie otrzyma pozycje, których tamto jeszcze nie pobrało, oraz wszystkie nowe."
    : "Włączyć tryb prywatny na tym urządzeniu?\n\nWszystkie pozycje z serwera zostaną przeniesione do tej przeglądarki i usunięte z serwera. Historia będzie dostępna tylko tutaj. Utrata urządzenia lub wyczyszczenie danych przeglądarki oznacza utratę historii.";
  if (!window.confirm(text)) return;
  busy(true);
  say("Przenoszenie danych…");
  P.enable().then(function(result){
    if (result.status !== "ok") throw new Error(result.status);
    say("Tryb prywatny włączony. Przeniesiono " + result.received + " " + plural(result.received) + ".");
    setTimeout(reload, 1200);
  }).catch(function(error){
    busy(false);
    say(error.message === "not_installed" ? "Otwórz Herring jako zainstalowaną aplikację, aby włączyć tryb prywatny." : "Nie udało się włączyć trybu prywatnego (" + error.message + ").", true);
  });
});

disableButton.addEventListener("click", function(){
  var bound = disableButton.dataset.bound === "1";
  var text = bound
    ? "Wyłączyć tryb prywatny?\n\nHistoria zapisana w tej przeglądarce zostanie usunięta i nie wróci na serwer. Serwer zacznie przechowywać nowe pozycje."
    : "Wyłączyć tryb prywatny?\n\nHistoria zostanie na powiązanym urządzeniu i nie wróci na serwer. Serwer zacznie przechowywać nowe pozycje.";
  if (!window.confirm(text)) return;
  busy(true);
  var action = bound ? P.disable() : fetch("/api/privacy/disable", {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}", credentials: "same-origin"}).then(function(r){ if (!r.ok) throw new Error("http_" + r.status); });
  action.then(reload).catch(function(error){ busy(false); say("Nie udało się wyłączyć trybu prywatnego (" + error.message + ").", true); });
});

wipeButton.addEventListener("click", function(){
  if (!window.confirm("Usunąć wszystkie dane Herring z tej przeglądarki?\n\nTej operacji nie można cofnąć.")) return;
  busy(true);
  P.wipe().then(function(){ say("Usunięto dane z tej przeglądarki."); busy(false); return refresh(); })
    .catch(function(error){ busy(false); say("Nie udało się usunąć danych (" + error.message + ").", true); });
});

refresh();
})();
`
