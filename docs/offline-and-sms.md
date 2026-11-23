# Offline alerts, SMS positions and account balance

Configure these per device in **Miejsca i powiadomienia**, in the
**Brak sygnału** section.

## Offline alert

A tracker in GPRS mode sends a position at least every
`HERRING_TRACKER_STOPPED_INTERVAL` seconds (300 by default), even when parked.
If no frame arrives for longer than the configured number of minutes, Herring
sends a push notification:

> Auto nie odzywa się od 45 minut. Możliwe przyczyny: brak zasięgu, zasilania
> albo środków na karcie SIM.

- Set 0 to turn the alert off, or a value from 10 minutes to 7 days. Use at
  least 30 minutes to stay clear of the stopped interval and short coverage
  gaps.
- Each outage is reported once. When frames resume, Herring sends
  "Auto znów jest w kontakcie."
- A background check runs every minute, so the alert can arrive up to a minute
  after the threshold.
- A device is watched only after it has sent at least one frame.
- The last contact time is stored in `device_contact`, separately from
  positions. It holds times only, so offline alerts keep working in privacy
  mode, where positions leave the server. Offline, recovery and SMS-position
  alerts never contain a location; place and speed alerts stay paused in
  privacy mode.

## Position by SMS

When the tracker has no mobile data but still has GSM coverage (an expired
data package, a weak signal, a roaming block), it can still answer by SMS.

- **Manually**: open the device's popup on the map and choose **Zapytaj SMS-em o
  pozycję**. For a device without any position, the same action appears in the
  message after clicking it in the list. Requests for one device are spaced at
  least two minutes apart.
- **Automatically**: tick **Gdy milczy, zapytaj go SMS-em o pozycję**. When the
  offline alert fires, Herring sends one request for that outage and the alert
  says so.

Herring sends `669` + the SMS password (`6690000` by default), the location
command in the ST-901 manuals. The tracker answers the control number, which
Herring set to its own gateway number during configuration, with a message
such as:

```text
http://maps.google.com/maps?q=+22.64208,+114.18828 Date: 2016-5-18 Time:13:40 ID: 8160528336 Fix:A Speed:0KM/H Bat:5
```

The webhook parses the map link (signs or N/S/E/W letters) or `Lat:`/`Lon:`
fields, plus `Fix`, `Speed`, `ID` and the date and time. The position is stored
with `source = 'sms'`, shown as **Pozycja z SMS-a** on the map, and handled by
retention and privacy mode like any other position. Users with an offline alert
or SMS fallback for the device get "Przyszła pozycja SMS-em od: Auto."

Details and limits:

- The firmware's clock zone is not documented. The SMS timestamp is used only
  if it lies within 30 minutes before (or 5 minutes after) receipt; otherwise
  the receive time is used, so a clock set to local time cannot make an SMS
  position look newer than real GPRS positions.
- `Fix:V` means no current GPS fix; the position is stored as invalid and the
  map shows **Brak fixu GPS**.
- If the device has no linked tracker yet, the `ID` in the reply links it, but
  only when that ID is registered (entered in the UI or listed in
  `HERRING_DEVICE_IDS`) and not claimed by another device.
- SMS positions do not trigger place or speed alerts.
- **Privacy**: an SMS position passes through the mobile operator and the SMS
  gateway in plain text, also in privacy mode. The settings page says so next
  to the option.
- **Cost**: each request is one SMS from the gateway plus one reply paid from
  the tracker's SIM.
- The manuals differ between ST-901 variants (2G, 4G, ST-901L/M). The command
  and reply format above follow the published manuals; verify them with the
  real device.

The SMS gateway retries a webhook until it receives `200 OK` (up to 36
attempts), so the webhook now answers `200` instead of `204`. A repeated
delivery of the same SMS is recognised and not stored twice.

## Checking the account balance

Not possible automatically, as far as the documentation goes:

- **Tracker SIM**: the ST-901 manuals list no balance command, and the
  tracker cannot run operator USSD codes such as `*100#`. Some other tracker
  brands forward a balance query to the operator (for example Coban's
  `balance+password …`), but no ST-901 equivalent is documented.
- **SMS gateway account**: the account is prepaid, but its published API
  documents only sending (`POST /api/sms`) and webhooks; there is no balance
  endpoint.

What Herring offers instead:

- **Ustawienia → SMS-y i stan konta** shows how many SMS messages Herring sent
  to trackers in the last 30 days, which together with the gateway's price
  list gives the cost.
- The offline alert names an empty SIM as one possible cause, which is how a
  prepaid SIM running out usually shows up first.

Check balances at the mobile operator and in the gateway's panel. A SIM with a
monthly plan instead of top-ups avoids the problem.
