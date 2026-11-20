# analytics

Tiny, cookieless, self-hosted analytics: a Go server with an embedded dashboard, plus a typed client of about 1.2 KB.
Production instance: **https://pulse.widacki.me**

| | |
|---|---|
| [USAGE.md](USAGE.md) | Add tracking to a site (script tag, React, Solid, Astro, vanilla, TS), the API, and what is collected |
| [DOKPLOY.md](DOKPLOY.md) | Deploy and operate the server on Dokploy behind Cloudflare, plus backups to R2 |

```
client/       TS library ("minalytics") + drop-in <script> build      → pnpm test / pnpm build
server/       Go server, SQLite, embedded dashboard + a.js           → go test ./...
Dockerfile    builds the server image (about 16 MB, FROM scratch)    → context = repo root
compose.yaml  Dokploy "Compose" deploys; add compose.local.yaml to run locally
```

## Development
```sh
cd client && pnpm i && pnpm test && pnpm build   # build refreshes client/dist + server/public/a.js; commit both
cd server && go test ./... && DASHBOARD_PASSWORD=x DB_PATH=./dev.db go run .
DASHBOARD_PASSWORD=x docker compose -f compose.yaml -f compose.local.yaml up --build   # http://localhost:8080
```
CI (`.github/workflows/ci.yml`) runs both test suites, checks that `a.js` is up to date, and pushes `ghcr.io/angael/analytics:latest` on main.
