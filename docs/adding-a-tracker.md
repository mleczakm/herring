# Adding a tracker

This guide covers adding an ST-901 (2G or 4G) to a running Herring instance.
Two separate things must happen before a tracker appears on the map:

1. **Allowlist** — its tracker ID must be in `HERRING_DEVICE_IDS`, otherwise
   every position it sends is rejected.
2. **Configure and link** — adding it in the UI sends the configuration SMS
   messages and creates the dashboard entry. The first position from an
   allowlisted ID is then linked to that entry automatically.

Adding the tracker in the UI does **not** allowlist it. Do step 1 first.

## Before you start

The server side must already be configured (see the
[README](../README.md) and [deployment notes](deployment.md)):

| Variable | Purpose |
| --- | --- |
| `HERRING_SENDLY_TOKEN`, `HERRING_SENDLY_FROM` | Sending SMS. Without both, the "Dodaj tracker" form is replaced by a notice. |
| `HERRING_SENDLY_WEBHOOK_SECRET` | Receiving the tracker's SMS reply. Without it, the tracker never reaches **configured**. |
| `HERRING_TRACKER_APN` | The SIM operator's APN, sent with command `803`. |
| `HERRING_TRACKER_PUBLIC_HOST` | Public IPv4 address the tracker connects to (command `804`). Not the Cloudflare-proxied web hostname. |
| `HERRING_TRACKER_PUBLIC_PORT` | Public raw-TCP port routed to container port `8090`. |
| `HERRING_TRACKER_PASSWORD` | Only if the tracker's SMS password is not the factory `0000`. |

In Sendly, the incoming-message and delivery webhook must point to
`https://śledź.mleczki.pl/webhooks/sendly/<HERRING_SENDLY_WEBHOOK_SECRET>`.

The tracker's SIM card needs:

- mobile data enabled and enough credit for data and SMS;
- the PIN lock disabled (check it in a phone first);
- the ability to send SMS replies to the Sendly number in `HERRING_SENDLY_FROM`.

## 1. Find the tracker ID

The tracker ID is the identifier the ST-901 puts in every position frame
(`*HQ,<ID>,V1,…#`). On SinoTrack devices it is usually printed on the label as
**ID** and is normally the last 10 digits of the IMEI. Use one of these to
confirm it:

- **Label** — the `ID` value on the device or its box.
- **RCONF reply** — the tracker's reply to `RCONF` starts with `ID:<ID>`. You
  can see it in the Sendly panel after step 3, or by texting `RCONF` to the SIM
  from a phone.
- **Herring logs** — when an unknown tracker connects, Herring logs
  `could not handle tracker location` with its `device_id` and a
  `FOREIGN KEY constraint failed` error. That `device_id` is the value to add.

## 2. Allowlist the ID

Add the ID to the comma-separated `HERRING_DEVICE_IDS` list and keep the
existing IDs. Removing an ID from the list does not revoke it: registration
only ever adds IDs to the database, so a once-listed tracker stays accepted.

- **Production** — edit the `DOTENV` secret of the `production` GitHub
  environment, then redeploy: rerun the latest “Production Build and Deploy”
  workflow (see [Manual deployment](deployment.md#manual-deployment)).
  IDs are read only at startup, so a restart is required.
- **Local** — export the variable and restart `go run ./cmd/herring`.

## 3. Add the tracker in the UI

1. Log in, open **Dodaj nowy tracker**.
2. Choose the variant (**ST-901 2G** or **ST-901 4G**).
3. Enter the SIM's phone number **with the country code**, e.g.
   `+48 600 100 200`. Spaces, dashes, brackets and `+` are removed; 7–15 digits
   must remain. The country code matters: the reply is matched by comparing
   this number with the sender number Sendly reports, and a number saved
   without it never matches.
4. Optionally give it a name; otherwise the dashboard shows the phone number.
5. Click **Dodaj i skonfiguruj przez SMS**.

Herring then sends these SMS messages, in order (`0000` is the password):

| Step | Command | Effect |
| --- | --- | --- |
| `control_number` | `<HERRING_SENDLY_FROM>0000 1` | Sets the Sendly number as the control number. |
| `apn` | `8030000 <APN>` | Sets the APN. |
| `server` | `8040000 <host> <port>` | Sets the TCP endpoint. |
| `moving_interval` | `8050000 <seconds>` | Report interval while moving (`HERRING_TRACKER_MOVING_INTERVAL`, default 20). |
| `stopped_interval` | `8090000 <seconds>` | Report interval while stopped (`HERRING_TRACKER_STOPPED_INTERVAL`, default 300). |
| `gprs_mode` | `7100000` | Switches the tracker to GPRS (TCP) reporting. |
| `verify` | `RCONF` | Asks the tracker to reply with its current configuration. |

## 4. Wait for confirmation

The dashboard shows the configuration state:

| State | Dashboard | Meaning |
| --- | --- | --- |
| `sending` | ⏳ Konfigurowanie… | SMS messages are being handed to Sendly. |
| `awaiting_reply` | ⏳ Konfigurowanie… | Sendly accepted all messages; waiting for the tracker's `RCONF` reply. |
| `configured` | Oczekiwanie na pierwszy sygnał, then speed / Postój | The reply confirmed GPRS mode, the APN, and `host:port`. Once a position is linked, the label shows speed, Postój (stopped), Brak fixu GPS, or Brak sygnału (no report for 15 minutes). |
| `failed` | ⚠ Konfiguracja nieudana | See [Troubleshooting](#troubleshooting). |

Separately from this state, the first frame from an allowlisted ID is linked to
the tracker and the marker appears on the map. **Linking happens only while
exactly one tracker is waiting for its first frame.** If two trackers are both
waiting, neither is linked, because Herring cannot tell them apart by phone
number. Add trackers one at a time: wait until a tracker shows a position before
adding the next.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| “Administrator serwera musi najpierw skonfigurować integrację Sendly.” | `HERRING_SENDLY_TOKEN` or `HERRING_SENDLY_FROM` is missing. |
| `failed`: “Niepełna konfiguracja serwera.” | The tracker profile is invalid: missing APN, a bad host, port, or intervals (10–18000 s), or a non-4-digit password. The log shows the exact reason. |
| `failed`: “Nie udało się wysłać komendy „…”.” | Sendly rejected the request (token, credit, or the number). Messages before the failing one were already sent. |
| Stays at `awaiting_reply` | No reply reached Herring: the webhook URL or secret is wrong in Sendly, the SIM cannot send SMS, the tracker is off or has no signal, or the phone number was saved without a country code. Check the Sendly panel for an inbound reply. |
| `failed`: “Tracker odpowiedział, ale ustawienia nie zgadzają się…” | The tracker replied, but its APN, mode, or `host:port` differ from the server profile, e.g. it rejected a command because its password is not `0000`. |
| Configured, but stuck at “Oczekiwanie na pierwszy sygnał” | The ID is not allowlisted (log shows `FOREIGN KEY constraint failed` with the `device_id`), the public TCP port is not reachable, or another tracker was also waiting for its first frame. |

### Retrying

There is no retry or remove action in the UI yet, and a phone number can only
be added once. To start over, delete the tracker's row while Herring is stopped
and after taking a backup (see [SQLite persistence](deployment.md#sqlite-persistence-and-recovery)):

```sql
PRAGMA foreign_keys = ON;  -- the sqlite3 shell disables it by default
DELETE FROM managed_devices WHERE phone_number = '48600100200';
```

Use the number as stored: digits only, including the country code. This also
deletes that tracker's SMS command history. Recorded positions are kept; they
belong to the tracker ID and are shown again once the tracker is linked.

To link a tracker by hand, for example when two were waiting at once:

```sql
UPDATE managed_devices SET tracker_id = '1234567890' WHERE phone_number = '48600100200';
```

## Adding a new tracker model

The UI and database currently accept only `st901-2g` and `st901-4g`. A new
model needs changes in several places:

- `internal/storage/sqlite/store.go` — the `managed_devices.model` `CHECK`
  constraint. `CREATE TABLE IF NOT EXISTS` does not alter existing databases,
  so this also needs a migration.
- `internal/httpapi/server.go` — the model allowlist in `createDevice`.
- `internal/httpapi/pages.go` — the `<option>` in the add-tracker form.
- `internal/tracker/` — the SMS command builder and the reply check, if the
  model does not use the ST-901 commands.
- `internal/protocol/` — a parser, if it does not speak H02. Add fixtures with
  their provenance to [protocol fixtures](protocol-fixtures.md).
