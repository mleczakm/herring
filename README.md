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
[architecture notes](docs/architecture.md).

## Development

Requirements: Go 1.24 or newer.

```shell
go test ./...
go run ./cmd/herring
```

The process exposes HTTP on `:8080` and the SinoTrack TCP listener on `:8090`.
Override them with `HERRING_HTTP_ADDR` and `HERRING_TRACKER_ADDR`. The HTTP
health probe is `GET /healthz`.

Build the production container with:

```shell
docker build -t herring .
```

Herring is licensed under the MIT License.
