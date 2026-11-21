# Using analytics

The server lives at `https://pulse.widacki.me`. The neutral hostname is deliberate: blocklists match hostnames like `analytics.*`, `stats.*` and `track.*`, and the paths `/a.js` and `/e` are neutral too.

Each site you track needs a **site key**, a short id such as `blog` or `portfolio`. If the server has `SITES` set, the key must be on that list (see [DOKPLOY.md](DOKPLOY.md#add-a-new-site)). Otherwise events are silently dropped.

## Option A: script tag (zero install, any site)

```html
<script defer src="https://pulse.widacki.me/a.js" data-site="blog"></script>
```

- Pageviews, including SPA navigations, are tracked automatically.
- Click tracking needs no JS: `<a href="/pricing" data-track="cta" data-track-place="hero">`.
- To track from code, call `window.minalytics.track('signup', { plan: 'pro' })`.
- Optional attributes: `data-endpoint` (defaults to the script's origin), `data-auto="false"` (no automatic pageviews), `data-dnt="true"` (respect Do Not Track / GPC).

## Option B: npm package (typed)

The repository is public, so install the package straight from git. No credentials are needed.

```sh
pnpm add "github:Angael/analytics#main&path:client"
```
The built `dist/` is committed, so nothing runs on install. npm and yarn can't install from a git subdirectory; use pnpm, or Option A.
> **CI/CD builds** (Vercel, Cloudflare Pages, Dokploy) install it the same way, with no token.

Create one module per app:

```ts
// src/analytics.ts
import { createAnalytics } from 'minalytics'

/** Every custom event this app sends. Use `void` for events without props. */
type Events = {
  signup: { plan: 'free' | 'pro' }   // props required
  newsletter: { source?: string }    // props optional
  logout: void                       // no props
}

export const analytics = createAnalytics<Events>({
  endpoint: 'https://pulse.widacki.me',
  site: 'blog',
})
```

```ts
analytics.track('signup', { plan: 'pro' }) // ✅ autocompleted
analytics.track('signin')                  // ❌ compile error: unknown event
analytics.track('signup')                  // ❌ compile error: props required
```

If you prefer a singleton without types, use `init(options)` once, then `track(name, props?)` and `pageview(path?)` anywhere.

### Per framework

Every function is SSR-safe: on the server it does nothing. Automatic pageviews hook `history.pushState` and `popstate`, so no router integration is needed.

**React (Vite, Next, React Router):** import the module once at the entry point.
```tsx
// main.tsx
import './analytics'
```
For Next.js App Router, do it in a client component rendered by the root layout:
```tsx
'use client'
import './analytics'
export function Analytics() { return null }
```

**Solid 2 / SolidStart:** import it in `index.tsx` / `entry-client.tsx`, or in the root component's `onMount(() => import('./analytics'))`.

**Astro:** add it to the base layout. Astro bundles the script and runs it once per full load, and view transitions (`<ClientRouter />`) are tracked automatically.
```astro
<script>
  import '../analytics'
</script>
```
Static Astro sites can also use Option A.

**Vanilla JS / TS:** `import { init } from 'minalytics'; init({ endpoint: 'https://pulse.widacki.me', site: 'blog' })`.

### Options

| option | default | |
|---|---|---|
| `endpoint` | required | `https://pulse.widacki.me` |
| `site` | required | site key |
| `autoPageviews` | `true` | initial pageview, `pushState` and `popstate` |
| `clickAttribute` | `"data-track"` | `false` to disable declarative click events |
| `ignoreLocalhost` | `true` | nothing is sent from `localhost`, `127.*` or `*.local` |
| `respectDnt` | `false` | honour Do Not Track / Global Privacy Control |
| `beforeSend` | – | `(payload) => payload \| false`, to rewrite (for example `/user/123` → `/user/:id`) or drop events |
| `debug` | `false` | `console.log` each event |

**Exclude yourself:** on each of your sites, run `localStorage.setItem('minalytics:ignore', '1')` in devtools, or call `ignoreThisBrowser()`.

## What is collected

| field | how |
|---|---|
| time | server clock |
| site, path | `location.pathname` only (no query string or hash, so tokens in URLs never leak) |
| event name + props | flat `string \| number \| boolean`; keep values low-cardinality (no user ids) |
| referrer | host of an external referrer, or `?utm_source=` / `?ref=` |
| country | Cloudflare `CF-IPCountry` |
| device / browser / OS | coarse User-Agent buckets plus screen width |
| visitor | `sha256(daily salt, IP, UA, site)`; the salt is deleted daily |

No cookies are set and raw IPs are never stored, so no consent banner is needed for this data. Uniques are **per day**, so a 30-day "visitors" number is the sum of daily uniques. Bots, headless browsers and prefetches are dropped server-side.

## Dashboard

Open `https://pulse.widacki.me/` and log in with basic auth (`DASHBOARD_USER` / `DASHBOARD_PASSWORD`). Pick the site and a range: today, 48h, 7d, 30d, 90d or 12 months.

The raw JSON is at `GET /api/stats?site=blog&from=<unix>&to=<unix>&tz=<getTimezoneOffset()>` (same auth).

## Wire protocol (for non-JS senders)

`POST https://pulse.widacki.me/e` with a url-encoded `text/plain` body, or the same fields as a `GET` query string (returns a 1×1 gif, usable as `<img>` in emails or noscript):

| key | value |
|---|---|
| `s` | site key |
| `t` | `p` pageview, `e` event |
| `p` | path |
| `n` | event name (`t=e`) |
| `r` | referrer (`t=p`) |
| `w` | screen width |
| `d` | url-encoded props, e.g. `plan%3Dpro` |
