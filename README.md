# Herring

Herring is a self-hosted GPS tracking service written in Go. The first supported
device family is SinoTrack ST-90x, starting with ST-901.

The project is at an early stage. The initial milestone covers:

- receiving ST-901 positions over TCP;
- displaying the latest position and history;
- configuring a tracker through Sendly SMS;
- ingesting tracker SMS replies through a Sendly webhook;
- offline, movement, geofence, and daily-summary notifications;
- installable PWA and Web Push notifications.

See [the implementation plan](docs/plan.md) and
[architecture notes](docs/architecture.md). Production releases use the
[Mikrus deployment process](docs/deployment.md).

## Development

Requirements: Go 1.24 or newer.

```shell
go test ./...
go run ./cmd/herring
```

The process exposes HTTP on `:8080` and the SinoTrack TCP listener on `:8090`.
Override them with `HERRING_HTTP_ADDR` and `HERRING_TRACKER_ADDR`. The HTTP
health probe is `GET /healthz`.

Positions are stored in SQLite at `herring.db`. Set `HERRING_DATABASE_PATH` to
change the location and provide a comma-separated allowlist of tracker IDs in
`HERRING_DEVICE_IDS`; frames from other IDs are rejected by the database
foreign-key boundary.

On a new database, opening `/` redirects to `/setup`, where the first
administrator can be registered. When `HERRING_ENV=production`, an empty
installation refuses to start without `HERRING_SETUP_TOKEN`; the setup form
requires that token and closes permanently after the administrator is created.

After login, adding an ST-901 needs only its variant and SIM number. Herring
uses the installation profile to send the control number, APN, TCP endpoint,
reporting intervals, GPRS mode and a final `RCONF` query through Sendly. The UI
shows the live state and only reports **configured** after the tracker's inbound
reply matches the expected APN and endpoint—not merely after Sendly accepts or
delivers the outgoing messages.

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
Sendly does not authenticate or retry webhooks, so the secret URL must not be
published. The tracker password defaults to `0000`; it can be overridden with
`HERRING_TRACKER_PASSWORD`.

Build the production container with:

```shell
docker build -t herring .
```

Herring is licensed under the MIT License.
