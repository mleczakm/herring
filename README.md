<img src="internal/httpapi/assets/herring.svg" width="56" height="56" alt="">

# Herring

Herring is a self-hosted GPS tracking service written in Go. The first supported
device family is SinoTrack ST-90x, starting with ST-901.

The project is at an early stage. The initial milestone covers:

- receiving ST-901 positions over TCP;
- displaying the latest position and history;
- configuring a tracker through Sendly SMS;
- ingesting tracker SMS replies through a Sendly webhook;
- saved places, frequent-stop suggestions, and opt-in arrival, departure, and speed alerts;
- installable PWA and Web Push notifications;
- an optional privacy mode that keeps location history only in one installed
  browser, and retention periods for the server and the browser.

See [the implementation plan](docs/plan.md),
[architecture notes](docs/architecture.md),
[adding a tracker](docs/adding-a-tracker.md), and
[privacy mode and data retention](docs/privacy.md), [offline alerts and positions by SMS](docs/offline-and-sms.md). The [brand guide](docs/branding.md) covers the voice, palette and logo. See [browser notifications](docs/notifications.md) for delivery and optional Gemini settings. Production releases use the
[Mikrus deployment process](docs/deployment.md).

## Development

Requirements: Go 1.24 or newer.

```shell
go test ./...
HERRING_TRACKER_PUBLIC_PORT=8090 go run ./cmd/herring
```

The process exposes HTTP on `:8080` and the SinoTrack TCP listener on `:8090`.
Override them with `HERRING_HTTP_ADDR` and `HERRING_TRACKER_ADDR`. The HTTP
health probe is `GET /healthz`.

Positions are stored in SQLite at `herring.db`. Set `HERRING_DATABASE_PATH` to
change the location. Only registered tracker IDs may send positions; frames
from other IDs are rejected by the database foreign-key boundary. IDs are
registered when a tracker is added in the UI, or at startup from the optional
comma-separated `HERRING_DEVICE_IDS`.

Open [the local app](http://localhost:8080/) for the Polish introduction and
installation entry point. On a new database, **Zacznij konfigurację** opens
`/setup` to create the first administrator. Returning visitors can sign in,
and authenticated users go straight to the live map. The public introduction
is always available at `/o-projekcie`. When `HERRING_ENV=production`, an empty
installation refuses to start without `HERRING_SETUP_TOKEN`; the setup form
requires that token and closes permanently after the administrator is created.

After login, an ST-901 is added with its variant and SIM number. Herring
uses the installation profile to send the control number, APN, TCP endpoint,
reporting intervals, GPRS mode and a final `RCONF` query through Sendly. The UI
shows the live state and only reports **configured** after the tracker's inbound
reply matches the expected APN and endpoint—not merely after Sendly accepts or
delivers the outgoing messages. Its tracker ID is entered in the same form, or
picked later from recently rejected IDs; until then its positions are
rejected. The **Urządzenia** page renames trackers, resends the configuration
and removes trackers. See
[Adding a tracker](docs/adding-a-tracker.md) for the full procedure and
troubleshooting.

The production `DOTENV` secret must also contain:

```dotenv
HERRING_SENDLY_TOKEN=...
HERRING_SENDLY_FROM=48500100200
HERRING_SENDLY_WEBHOOK_SECRET=<random-high-entropy-value>
HERRING_TRACKER_APN=internet
HERRING_TRACKER_PUBLIC_HOST=65.108.44.244
```

Configure Sendly's incoming-message and delivery webhook as
`https://śledź.mleczki.pl/webhooks/sendly/<HERRING_SENDLY_WEBHOOK_SECRET>`.
Sendly does not authenticate webhooks, so the secret URL must not be
published. It retries a webhook until it receives `200 OK`; Herring answers
`200` and ignores repeated deliveries. The tracker password defaults to `0000`; it can be
overridden with `HERRING_TRACKER_PASSWORD`.

Build the production container with:

```shell
docker build -t herring .
```

Herring is licensed under the MIT License.
