# Deploying on Dokploy

Target: `https://pulse.widacki.me` on a Dokploy VPS, proxied by Cloudflare.
The image is about 16 MB and uses about 15 MB of RAM. Data is a single SQLite file of roughly 40–60 MB per million events.

## 1. DNS (Cloudflare)

- Add an `A` record: `pulse` → VPS IP, **Proxied (orange cloud)**. The proxy is required because the server reads `CF-IPCountry` and `CF-Connecting-IP`.
- Set SSL/TLS mode to **Full (strict)**. Dokploy/Traefik gets a Let's Encrypt cert. If HTTP-01 fails behind the proxy, set the domain's certificate to *None* in Dokploy and use a Cloudflare Origin Certificate, or temporarily grey-cloud the record while issuing.
- Optional: under **Rules → Configuration rules** for `pulse.widacki.me`, disable *Rocket Loader* and *Email obfuscation*. Leave caching on. `/a.js` sends `Cache-Control: public, max-age=86400`, and `/e` is a POST that is never cached.

## 2. Create the app (recommended: Application + Dockerfile)

1. **Project → Create Service → Application**, name it `analytics`.
2. **Provider:** GitHub → repository `Angael/analytics`, branch `main`. Install the Dokploy GitHub App on the private repo if you haven't. Enable **Autodeploy**.
3. **Build type:** `Dockerfile`, Dockerfile path `Dockerfile`, build context `.` (repo root). The build also runs the Go tests, so a broken commit never deploys.
4. **Environment:**
   ```env
   DASHBOARD_PASSWORD=<long random>         # openssl rand -base64 24
   DASHBOARD_USER=admin
   SITES=blog,portfolio                     # allow-list; anything else is dropped
   RETENTION_DAYS=0                         # 0 = keep forever (storage is tiny)
   # Optional nightly backups to Cloudflare R2:
   R2_ACCOUNT_ID=
   R2_BUCKET=
   R2_ACCESS_KEY_ID=
   R2_SECRET_ACCESS_KEY=
   R2_PREFIX=analytics/
   ```
   `ADDR` (`:8080`) and `DB_PATH` (`/data/analytics.db`) are already set in the image.
5. **Advanced → Mounts:** add a **Volume mount**, volume name `analytics-data`, mount path `/data`. **Without this, all data is lost on every redeploy.** Use a named volume rather than a bind mount: the container runs as uid 65534, and a new named volume inherits the right ownership from the image. If you must bind-mount, run `chown 65534:65534` on the host directory first.
6. **Domains:** host `pulse.widacki.me`, path `/`, container port **8080**, HTTPS on, certificate `letsencrypt` (or *None* when using a Cloudflare origin cert, see above).
7. **Advanced → Resources (optional):** memory limit 128 MB.
8. **Deploy.** Check that `https://pulse.widacki.me/health` returns `ok` and that `/` asks for a password.

The image has a built-in `HEALTHCHECK` (`/analytics healthcheck`), so Dokploy's swarm rollout waits for it.

### Alternative: Compose service
**Create Service → Compose**, same repo, compose path `compose.yaml`. Set the env vars in the Environment tab, then add the domain in the Domains tab (service `analytics`, port `8080`). `compose.yaml` publishes no host ports, and Dokploy attaches Traefik. The volume `analytics-data` is declared in the file.

### Alternative: prebuilt image
CI pushes `ghcr.io/angael/analytics:latest` on every push to main. In Dokploy, choose **Provider: Docker**, image `ghcr.io/angael/analytics:latest`, and add registry credentials (GitHub username plus a PAT with `read:packages`, because the package is private). The env, mounts and domain are the same as above. This skips building on the VPS.

## Add a new site
1. Append the key to `SITES` in Dokploy (for example `blog,portfolio,newsite`) and **redeploy**. Env changes need a redeploy.
2. Add tracking to the site (see [USAGE.md](USAGE.md)).
3. The site appears in the dashboard selector after its first event.

## Backups (Cloudflare R2)
1. In R2, create a bucket (for example `backups`). Under **Manage R2 API Tokens**, create a token with *Object Read & Write* scoped to that bucket. Copy the Access Key ID, the Secret, and your Account ID.
2. Set the `R2_*` env vars and redeploy. Every day after 03:00 UTC the server writes `analytics/analytics-YYYY-MM-DD.db.gz`, a consistent `VACUUM INTO` snapshot compressed with gzip. Watch the logs for `backup uploaded`.
3. Add an R2 **lifecycle rule** on prefix `analytics/` to delete objects after 30 days. At about 1–5 MB per backup, that stays far inside the free tier.

### Restore
```sh
# On the VPS: download a backup, then:
gunzip analytics-2026-11-20.db.gz
docker service scale <app-service>=0     # or stop the app in Dokploy
docker run --rm -v analytics-data:/data -v "$PWD":/in alpine \
  sh -c 'rm -f /data/analytics.db* && cp /in/analytics-2026-11-20.db /data/analytics.db && chown 65534:65534 /data/analytics.db'
# start the app again in Dokploy
```
(Dokploy may prefix the volume name. Check with `docker volume ls | grep analytics`.)

## Operations
- **Logs:** Dokploy → app → Logs. Normal output is quiet: startup, daily `backup uploaded`, and `purged N events` if retention is on.
- **Size check:** `docker run --rm -v analytics-data:/d alpine du -h /d`.
- **Change the password:** edit `DASHBOARD_PASSWORD` and redeploy.
- **Rate limiting / abuse:** optionally add a Cloudflare WAF rate-limit rule on `pulse.widacki.me/e`, for example 60 requests per 10 s per IP. `SITES` already blocks garbage site keys.
- **Upgrades:** push to main. Autodeploy rebuilds, and the schema is created with `CREATE TABLE IF NOT EXISTS`, so no migrations are needed so far.
