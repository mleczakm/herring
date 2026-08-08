# Architecture

Herring starts as a modular monolith. This keeps deployment on a small VPS
simple while preserving boundaries that can later be split if load requires it.

```text
ST-901 -- TCP/H02 --> ingest --> positions/device state --> alert evaluator
   ^                                      |                    |
   |                                      v                    v
Sendly <-- REST SMS -- command service   HTTP API          outbox worker
   |                    ^                  |                    |
   +-- webhook replies -+                  +--> PWA <-- Web Push+
                                            |
                                         PostgreSQL
```

## Runtime components

- `cmd/herring`: process lifecycle, configuration, health checks, and graceful
  shutdown.
- `internal/protocol/sinotrack`: pure frame decoding and acknowledgements; no
  database or network dependencies.
- `internal/ingest`: bounded TCP sessions, framing, device lookup, validation,
  deduplication, and persistence.
- `internal/sms`: ST-901 command builders, Sendly adapter, webhook handling, and
  command/reply audit.
- `internal/alerts`: deterministic transition rules evaluated from persisted
  positions and scheduled checks.
- `internal/notify`: transactional outbox consumers and Web Push delivery.
- `internal/httpapi`: authentication, device/history/configuration API, static
  PWA assets, and webhook endpoint.

## Data model outline

- `users`: account and locale/time-zone preferences.
- `devices`: owner, label, tracker identifier, phone number, model, last contact,
  last valid position, and desired reporting policy.
- `positions`: device, tracker time, received time, coordinates, speed, heading,
  validity, protocol type/status, raw-frame reference, and deduplication key.
- `geofences`: owner, name, circle/polygon geometry, and rule settings.
- `device_geofence_state`: last stable inside/outside state and transition time.
- `alert_rules` / `alert_events`: configuration and immutable transitions.
- `sms_commands` / `sms_events`: redacted command audit and provider state.
- `push_subscriptions`: endpoint and encrypted Web Push key material.
- `outbox`: durable notification jobs with attempts and next-attempt time.

## Security boundaries

- Never expose the device SMS password in logs or API responses after creation.
- Only predefined, validated ST-901 commands are allowed by default; arbitrary
  SMS requires a privileged diagnostic path.
- TCP device identifiers are not authentication secrets. Unknown identifiers are
  rejected/audited and each listener is rate- and size-limited.
- Sendly tokens, database credentials, session keys, and VAPID private keys come
  from secret files/environment and are never stored in Git.
- Webhook input is untrusted even when source-IP filtered. Body size, JSON shape,
  phone numbers, type, and correlation are validated.
- Public HTTP traffic requires HTTPS; security headers and same-site secure
  sessions are enabled at the application/reverse proxy boundary.

## Reliability rules

- The TCP parser operates on complete delimited frames and supports multiple or
  fragmented frames per connection.
- Database writes and outbox creation share a transaction.
- Duplicate device frames and webhook deliveries are harmless.
- Alert state is persisted, so restarts do not repeat movement/geofence events.
- Daily summaries are scheduled in the user's time zone with DST-safe semantics.
