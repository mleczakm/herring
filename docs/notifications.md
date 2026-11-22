# Web Push notifications

In the dashboard, open **Miejsca i powiadomienia**. Choose a point on the map, name it and set its radius. Suggestions use valid slow or stationary fixes near the same point for at least five minutes on two separate days. Select places for each tracker, choose arrivals or departures, and optionally set a speed threshold. The first fix establishes the initial state. A new crossing triggers a notification.

Server-side suggestions and alerts pause in private mode. Manually saved places and rules remain in settings. Enabling or disabling private mode clears the last alert state so movement during private mode does not produce later alerts.

Web Push requires HTTPS (or localhost for development), a browser with Push API support, and notification permission. A VAPID key pair is generated once and stored in the SQLite database automatically. Optionally configure a stable key pair and contact subject on the server:

- `HERRING_VAPID_PUBLIC_KEY`: URL-safe base64 uncompressed P-256 public key.
- `HERRING_VAPID_PRIVATE_KEY`: URL-safe base64 P-256 private key. Keep it secret and stable; changing the pair requires browsers to subscribe again.
- `HERRING_VAPID_SUBJECT`: contact URI, such as `mailto:admin@example.com` or an HTTPS URL; defaults to the project URL.
- `HERRING_GEMINI_MODEL` (optional): model name; defaults to `gemini-3.5-flash-lite`.
- `HERRING_GEMINI_API_KEY` (or `GEMINI_API_KEY`): enables Gemini humor for alerts explicitly configured to use it. Without it, or if Gemini fails, a local humorous Polish phrase is used when humor is enabled.

If supplying a pair yourself, generate it using a trusted Web Push key generator. The private key and Gemini key belong in the server's secret configuration, never the repository. The browser sends its push subscription to the authenticated same-origin server. Subscription endpoints are restricted to known browser push services (Google FCM, Mozilla, Apple, and Windows), so other providers are not accepted. Expired subscriptions are removed when a provider returns 404 or 410.

Gemini receives only a fixed generic prompt and one of three generic alert categories, never device or place names, coordinates, speed, identifiers, or location history. The factual alert is composed locally. Delivery is best effort, bounded by timeouts, and not a durable queue; a provider failure or server shutdown can lose a notification.

On iPhone and iPad, add Herring to the Home Screen and open it from there before enabling notifications. Permission must be requested from a user action. Other browser and operating-system settings may also affect delivery.

Subscription changes require an authenticated session and a non-null same-origin `Origin` header. The server validates the subscription endpoint and encryption keys before storing them. Delivery processes at most 100 subscriptions per user per alert, with a 15-second overall deadline; additional saved subscriptions are not included in that alert. The endpoint can be removed from Herring with the browser's unsubscribe control.
