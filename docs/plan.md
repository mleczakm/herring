# Herring implementation plan

## Verified feasibility

### ST-901 data

ST-901 can be switched to GPRS mode and configured with a destination IP and
TCP port. It sends textual H02/SinoTrack frames such as `*HQ,...#`. The first
implementation will parse location frames, validate coordinates and timestamps,
convert NMEA degrees/minutes to decimal degrees, and acknowledge accepted
messages. Raw frames will be retained for diagnostics.

The protocol has variants across firmware and 2G/4G devices. Captured frames
from the actual tracker are therefore an explicit acceptance-test input before
production use.

### Sendly SMS

Sendly documents both required directions:

- `POST https://api.sendly.link/api/sms` sends an SMS from an optional Sendly
  virtual mobile number;
- incoming SMS messages are delivered to a configured webhook as JSON with
  `type`, `from`, `to`, and `body`;
- delivery reports are sent to the same webhook with `message_id` and `status`.

This makes ST-901 configuration and reply parsing technically possible if the
account has a two-way virtual number that the tracker can reply to. This must be
confirmed with a real Sendly account and SIM before the feature is considered
production-ready.

Sendly states that webhook requests have no authorization, are attempted once,
and originate from the current IP address of `api.sendly.link`. Herring will
therefore use a high-entropy webhook path, match sender and destination numbers,
record an idempotency fingerprint, respond quickly before asynchronous parsing,
and optionally restrict source IPs at the reverse proxy. A dead-letter/audit
record is required because Sendly does not retry.

### Mobile notifications

An installable PWA can subscribe to standards-based Web Push through a service
worker. On iOS/iPadOS, Web Push is available for Home Screen web apps and the
permission request must follow a direct user action. HTTPS and VAPID keys are
required. The backend can use the same notification outbox for Web Push and
future channels.

## Milestones

### M0 — protocol spike

- Go module, configuration loading, structured logging, health endpoint.
- Streaming TCP listener with connection and frame size limits.
- ST-901 location parser and table-driven tests using sanitized fixtures.
- SQLite schema/migrations and CI checks.

Exit criterion: an ST-901 pointed at a test instance produces validated decoded
frames visible in logs without crashing or leaking connections.

### M1 — single-user tracking MVP

- SQLite schema for users, devices, positions, and device state.
- Authentication and device ownership.
- Position ingestion with raw-frame audit, deduplication, and retention policy.
- REST API and responsive map showing current position and history.
- PWA manifest, service worker, install guidance, and offline application shell.

Exit criterion: one user can add an ST-901, view its latest position and route,
and install Herring on a phone.

### M2 — SMS configuration

- Sendly client with timeouts, redacted logs, delivery status, and test double.
- Safe command builder for control number, GPRS mode, APN, server address,
  moving/stationary intervals, location request, and `RCONF`.
- Command history with explicit confirmation, status, reply correlation, and
  rate limits.
- Inbound/delivery webhook parser, audit, and operational alerts.

Exit criterion: configuration commands and responses complete an audited round
trip using the production Sendly number and a real ST-901.

### M3 — alert engine and Web Push

- Durable event/outbox pipeline; per-device time zone and quiet hours.
- No-contact rule with recovery event and configurable threshold.
- Movement start rule with speed/distance hysteresis and GPS-noise filtering.
- Circular and polygon geofences with enter/leave transitions and dwell/debounce.
- Daily summary (distance, movement duration, first/last contact, alerts).
- Web Push subscriptions, VAPID key rotation strategy, retries, and pruning of
  expired subscriptions.

Exit criterion: alert transitions are deterministic under replay and are
delivered to installed Android and iOS PWAs.

### M4 — production hardening

- Metrics, tracing, dashboards, backups, restore drill, and data export/deletion.
- Reverse-proxy TLS, secrets management, abuse controls, and security review.
- Load and soak tests, malformed-frame fuzzing, migrations and rollback runbook.
- VPS deployment manifests and zero/low-downtime release procedure.

## Initial product decisions

- Modular monolith first: one repository and deployable Go binary, with TCP and
  HTTP listeners and background workers separated by internal packages.
- SQLite is the source of truth. Enable WAL mode, foreign keys, a busy timeout,
  and a controlled write path. Store coordinates as validated numeric columns;
  evaluate circles/polygons in Go and add SQLite R*Tree indexes only when the
  measured device/geofence scale justifies them.
- Use SQLite's online backup mechanism or `VACUUM INTO` for consistent backups;
  copying only the main database file while WAL mode is active is not a backup.
- At-least-once processing with idempotent consumers and a transactional outbox.
- Server-side alert evaluation; device-native alarms may be ingested but are not
  the only source of truth.
- Store timestamps in UTC and retain the device-reported time and reception time.

## Open questions for field validation

- Exact ST-901 model/firmware (2G, 4G/LTE) and sample frames.
- SIM operator, APN, public DNS/IP behavior, and whether the firmware accepts a
  hostname or only an IPv4 address in command `804`.
- Sendly virtual number availability, two-way SMS support for the chosen plan,
  sender-number stability, encoding limits, and webhook source IP ranges.
- Expected number of devices, position interval, history retention, and VPS
  resource limits.
