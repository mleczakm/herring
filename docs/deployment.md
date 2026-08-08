# Production deployment

Herring follows the deployment process used by `cargo.mleczki.pl`:

1. CI succeeds on `main`.
2. The release workflow creates a timestamp tag using `PAT_TOKEN`.
3. A tag push builds a versioned image and `latest`, then pushes both to GHCR.
4. Ansible connects to Mikrus, pulls the image, replaces the container while
   retaining the `herring-data` volume, checks `/healthz`, updates Cloudflare,
   and verifies the Cytrus domain.

The public web name is `śledź.mleczki.pl`; DNS/API configuration uses its IDNA
form `xn--led-bza2n.mleczki.pl`.

## GitHub production configuration

Create the `production` environment and configure the same secrets as Cargo:

- `PAT_TOKEN` — token that can create a release/tag and trigger the tag workflow;
- `SSH_PRIVATE_KEY`, `MIKRUS_SSH_HOST`, `MIKRUS_SSH_PORT`, `MIKRUS_IPV6`;
- `CYTRUS_IPV4`, `CYTRUS_API_TOKEN`;
- `CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ZONE_ID`;
- `DOTENV` — production environment file content; initially it must contain
  `HERRING_DEVICE_IDS=<comma-separated tracker identifiers>`;
- `TRACKER_PUBLIC_PORT` — the host TCP port assigned/routed to this Mikrus VPS.

After the secrets, Cytrus domain, raw TCP port, and first backup are ready, set
the repository variable `RELEASE_ENABLED` to `true`. Until then, CI still runs
but the automatic release/deployment job is skipped. This prevents merging the
deployment scaffolding from accidentally starting an incomplete production
rollout.

`PAT_TOKEN` is intentionally used for the release because a tag created with the
workflow's default token does not trigger another workflow. Production secrets
should be restricted to the protected `production` environment.

## Network paths

- HTTPS: Cloudflare/Cytrus → Mikrus IPv6 port `8080` → container port `8080`.
- Tracker: public IPv4 and `TRACKER_PUBLIC_PORT` → container TCP port `8090`.

Cloudflare's regular HTTP proxy does not carry the tracker protocol. Before the
first release, confirm in Mikrus/Cytrus that `TRACKER_PUBLIC_PORT` is assigned and
reachable over raw TCP. Configure ST-901 command `804` with the public IPv4 and
that port, not the proxied web hostname.

## SQLite persistence and recovery

The named volume `herring-data` is mounted at `/data`; the application writes
`/data/herring.db` and its WAL sidecars. Replacing the container does not remove
the volume. Never use `docker volume rm herring-data` during a deployment.

A plain copy of only `herring.db` while Herring is running is not a consistent
backup in WAL mode. Add a SQLite-aware backup job before storing production
history; use the SQLite backup API or `VACUUM INTO`, then copy the resulting
snapshot off the VPS. A restore drill is a production-readiness requirement.

## Manual deployment

Normal releases are automatic after CI. To redeploy an existing tag, rerun its
“Production Build and Deploy” workflow in GitHub Actions. Creating a new tag
manually also starts deployment:

```shell
git tag YYYYMMDDHHMM
git push origin YYYYMMDDHHMM
```
