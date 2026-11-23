# Adding a tracker

This guide covers adding an ST-901 (2G or 4G) to a running Herring instance.
Two separate things must happen before a tracker appears on the map:

1. **Register its ID** — positions are accepted only from tracker IDs Herring
   knows. Frames from any other ID are rejected.
2. **Configure it** — Herring texts the tracker its connection settings and
   waits for the tracker to confirm them.

The **Dodaj lokalizator** form does both when you fill in the optional
**ID lokalizatora** field. No redeploy or environment change is needed.

## Before you start

The server side must already be configured (see the
[README](../README.md) and [deployment notes](deployment.md)):

| Variable | Purpose |
| --- | --- |
| `HERRING_SENDLY_TOKEN`, `HERRING_SENDLY_FROM` | Sending SMS. Without both, the add form is replaced by a notice and configuration cannot be resent. |
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
**ID** and is normally the last 10 digits of the IMEI. Herring accepts 5 to 20
digits; spaces are removed. Use one of these to find it:

- **Label** — the `ID` value on the device or its box.
- **Unknown trackers list** — once the tracker has its server settings, its
  first frames are rejected and its ID appears under the ID field in the add
  form and on the **Urządzenia** page (see [Unknown trackers](#unknown-trackers)).
- **RCONF reply** — the tracker's reply to `RCONF` starts with `ID:<ID>`. You
  can see it in the Sendly panel after step 2, or by texting `RCONF` to the SIM
  from a phone.
- **Herring logs** — a rejected frame is logged as
  `could not handle tracker location` with its `device_id` and the error
  `tracker ID is not registered`.

You can add the tracker without the ID and fill it in later on the
**Urządzenia** page.

## 2. Add the tracker in the UI

1. Log in and click **Dodaj lokalizator** under the device list on the map.
2. Choose the variant (**ST-901 2G** or **ST-901 4G**).
3. Enter the SIM's phone number **with the country code**, e.g.
   `+48 600 100 200`. Spaces, dashes, brackets and `+` are removed; 7–15 digits
   must remain. The country code matters: the reply is matched by comparing
   this number with the sender number Sendly reports, and a number saved
   without it never matches.
4. Optionally give it a name (up to 80 characters); otherwise the dashboard
   shows the phone number.
5. Optionally enter the **ID lokalizatora**, or click one of the recently
   rejected IDs listed under the field to fill it in.
6. Click **Dodaj i skonfiguruj przez SMS**.

The form is rejected with a message, and nothing is sent, when the ID is not
5–20 digits, the ID already belongs to another tracker, or the phone number is
already added.

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

## 3. Wait for confirmation

The dashboard shows the configuration state:

| State | Dashboard | Meaning |
| --- | --- | --- |
| `sending` | ⏳ Konfigurowanie… | SMS messages are being handed to Sendly. |
| `awaiting_reply` | ⏳ Konfigurowanie… | Sendly accepted all messages; waiting for the tracker's `RCONF` reply. |
| `configured` | Oczekiwanie na pierwszy sygnał, then speed / Postój | The reply confirmed GPRS mode, the APN, and `host:port`. Once a position is linked, the label shows speed, Postój (stopped), Brak fixu GPS, or Brak sygnału (no report for 15 minutes). |
| `failed` | ⚠ Konfiguracja nieudana | See [Troubleshooting](#troubleshooting). |

Separately from this state, positions are shown on the map as soon as the
tracker's ID is registered and linked:

- **ID entered in the form or on the Urządzenia page** — registered and linked
  at once; the first frame appears on the map.
- **No ID entered** — the tracker's frames are rejected and its ID shows up as
  an unknown tracker. Assign it on the **Urządzenia** page.
- **ID only in `HERRING_DEVICE_IDS`** — the first frame from that ID is linked
  automatically, but **only while exactly one tracker is waiting for its first
  frame**. If two trackers are both waiting, neither is linked, because Herring
  cannot tell them apart by phone number. Enter the ID on the Urządzenia page
  instead.

## Unknown trackers

When a frame arrives from an ID that is not registered, Herring rejects the
position and remembers the ID with the time of the first and last attempt and
the number of attempts. It stores no coordinates or addresses. The list keeps
IDs seen in the last 7 days, at most the 20 most recent, and an ID leaves it
as soon as it is assigned to a tracker.

The tracker port is public, so anyone can send frames with any ID. Herring
therefore never registers an ID on its own: an ID is accepted only after you
assign it in the add form or on the Urządzenia page. Pick only an ID that
matches the tracker's label or its `RCONF` reply.

## Managing trackers

The **Urządzenia** page (sidebar, or the link under the device list) shows each
tracker's variant, SIM number, ID and configuration state, with three actions.

### Rename or change the ID

Edit **Nazwa** or **ID lokalizatora** and click **Zapisz**. Leaving the ID
empty keeps the current one. Changing the ID of a tracker that already has one
unregisters the old ID and **deletes the positions stored under it**, because
they came from a different device; frames from the old ID are rejected from
then on.

### Resend the configuration

**Wyślij ustawienia ponownie** sends all configuration SMS messages again and
sets the state back to `awaiting_reply`. Use it after a `failed` state, or when
the tracker was off or without signal. Every message costs one SMS, so the
button refuses to send again within 5 minutes of the previous attempt.

### Remove a tracker

Tick the confirmation box and click **Usuń urządzenie**. Removing a tracker:

- deletes the tracker, its SMS command history, its alert settings and its
  offline-alert state;
- unregisters its ID and **deletes all of its positions from the server**,
  including positions received by SMS. Frames it still sends are rejected and
  its ID shows up as an unknown tracker again;
- frees the phone number, so the tracker can be added again.

It does not text the tracker, so the tracker keeps its settings and keeps
reporting until you switch it off. In privacy mode, positions already moved to
the bound browser stay there. An ID listed in `HERRING_DEVICE_IDS` is
registered again at the next start, so remove it from that list too.

## `HERRING_DEVICE_IDS`

Installations that list tracker IDs in `HERRING_DEVICE_IDS` keep working: the
IDs are registered at every start, and a listed ID is linked automatically as
described in [step 3](#3-wait-for-confirmation). New trackers do not need it.
Removing an ID from the list does not unregister it; remove the tracker in the
UI instead.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| “Aby dodać lokalizator, poproś administratora o włączenie wysyłki SMS.” | `HERRING_SENDLY_TOKEN` or `HERRING_SENDLY_FROM` is missing. |
| `failed`: “Niepełna konfiguracja serwera.” | The tracker profile is invalid: missing APN, a bad host, port, or intervals (10–18000 s), or a non-4-digit password. The log shows the exact reason. |
| `failed`: “Nie udało się wysłać komendy „…”.” | Sendly rejected the request (token, credit, or the number). Messages before the failing one were already sent. |
| Stays at `awaiting_reply` | No reply reached Herring: the webhook URL or secret is wrong in Sendly, the SIM cannot send SMS, the tracker is off or has no signal, or the phone number was saved without a country code. Check the Sendly panel for an inbound reply. |
| `failed`: “Tracker odpowiedział, ale ustawienia nie zgadzają się…” | The tracker replied, but its APN, mode, or `host:port` differ from the server profile, e.g. it rejected a command because its password is not `0000`. |
| Configured, but stuck at “Oczekiwanie na pierwszy sygnał” | The tracker has no ID or a wrong one (check **Nieznane lokalizatory** on the Urządzenia page, or the log for `tracker ID is not registered`), or the public TCP port is not reachable. |

To retry, use **Wyślij ustawienia ponownie** on the Urządzenia page. To start
over, for example after a wrong phone number, remove the tracker there and add
it again.

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
