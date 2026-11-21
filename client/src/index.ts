/**
 * Tiny, cookieless analytics client (~1.5 KB gzipped). Framework agnostic and SSR safe:
 * every function is a no-op when there is no `window`.
 *
 * Runs in regular websites and in desktop app renderers (Electron, Tauri, Capacitor): pages loaded
 * from `file:` or a custom `app://` scheme report clean routes instead of install paths, and hash
 * routers (`#/settings`) are tracked as real pages.
 *
 * @example Quick start (singleton)
 * ```ts
 * import { init, track } from 'minalytics'
 *
 * init({ endpoint: 'https://stats.example.com', site: 'blog' }) // pageviews are tracked automatically
 * track('signup', { plan: 'pro' })
 * ```
 *
 * @example Typed events
 * ```ts
 * import { createAnalytics } from 'minalytics'
 *
 * type Events = {
 *   signup: { plan: 'free' | 'pro' } // props required
 *   newsletter: { source?: string }   // props optional
 *   logout: void                      // no props
 * }
 * export const analytics = createAnalytics<Events>({ endpoint: '...', site: 'blog' })
 * analytics.track('signup', { plan: 'pro' }) // ✅
 * analytics.track('signin')                  // ❌ type error: unknown event
 * ```
 *
 * @module
 */

/** A single property value. `null`/`undefined` values are dropped. */
export type PropValue = string | number | boolean | null | undefined

/** Flat key/value properties attached to a custom event. Keep them short: they are stored as-is. */
export type Props = Record<string, PropValue>

/**
 * Map of event name → props shape. Use `void` for events without props.
 * Pass it as a generic to {@link createAnalytics} to get autocompletion and type checking for {@link Analytics.track}.
 */
export type EventMap = { [name: string]: Props | void | undefined }

/** Default, untyped event map: any event name, optional props. */
export type AnyEvents = Record<string, Props | undefined>

/** Resolves the rest-args of `track()`: props are optional when the shape allows `undefined`/`void` or has no required keys. */
export type TrackArgs<P> = undefined extends P
  ? [props?: Exclude<P, void>]
  : {} extends P
    ? [props?: P]
    : [props: P]

/** Options for {@link createAnalytics} / {@link init}. */
export interface AnalyticsOptions {
  /**
   * Base URL of your analytics server, e.g. `https://stats.example.com`.
   * Events are sent to `${endpoint}/e`.
   */
  endpoint: string
  /** Site key that groups data on the dashboard, e.g. `"blog"`. Must be allowed by the server's `SITES` (if set). */
  site: string
  /**
   * Automatically track the initial pageview and client-side navigations
   * (`history.pushState`, `popstate`). Works with React Router, Solid Router, Astro view transitions, etc.
   * @default true
   */
  autoPageviews?: boolean
  /**
   * Track clicks on elements with this attribute as custom events.
   * `<button data-track="signup" data-track-plan="pro">` sends event `signup` with `{ plan: "pro" }`.
   * Set `false` to disable.
   * @default "data-track"
   */
  clickAttribute?: string | false
  /**
   * Don't send anything from development hosts: `localhost`, `127.0.0.1`, `[::1]`, `*.local`, and `file:` pages
   * outside Electron. A packaged Electron app (loaded from `file:`) and custom app schemes (`app://`, `tauri://`)
   * count as production; an Electron app in dev mode (served from `localhost`) is ignored like any dev server.
   * @default true
   */
  ignoreLocalhost?: boolean
  /**
   * Master switch. `false` sends nothing at all (events still reach `debug` logging).
   * Use it for an "anonymous usage statistics" setting in a desktop app, or to gate on your own build flags.
   * @default true
   */
  enabled?: boolean
  /**
   * App or release version, e.g. `"1.4.0"` (max 32 chars). Shown as "App versions" on the dashboard, so you can see
   * which releases are in use. Most useful for desktop apps; in Electron pass `app.getVersion()` from the preload
   * script, or inject your `package.json` version at build time.
   */
  version?: string
  /**
   * Treat hash routes (`#/settings`, `#!/settings`) as pages. Plain anchors like `#pricing` are never tracked.
   * The route replaces the path when the page itself is `/`, otherwise it is appended: `/docs#/intro`.
   * Hash routing is the norm in desktop apps loaded from `file:`.
   * @default true
   */
  hashRoutes?: boolean
  /**
   * Don't send anything when the browser has Do Not Track / Global Privacy Control enabled.
   * @default false
   */
  respectDnt?: boolean
  /**
   * Hook to rewrite or drop events before sending. Return `false` to drop.
   * Useful to strip IDs from paths: `p => ({ ...p, path: p.path.replace(/\/\d+/g, '/:id') })`.
   */
  beforeSend?: (payload: Payload) => Payload | false
  /** Log events to the console instead of being silent. @default false */
  debug?: boolean
}

/** The event about to be sent. Exposed to {@link AnalyticsOptions.beforeSend}. */
export interface Payload {
  /** `"pageview"` or `"event"`. */
  type: 'pageview' | 'event'
  /** Page path (no query string). Includes the hash route when {@link AnalyticsOptions.hashRoutes} applies. */
  path: string
  /** Event name (custom events only). */
  name?: string
  /** Referrer URL or `utm_source` (pageviews only, only for external referrers). */
  referrer?: string
  /** Event props (custom events only). */
  props?: Props
}

/** An analytics client. Create one with {@link createAnalytics}. */
export interface Analytics<E extends EventMap = AnyEvents> {
  /**
   * Record a pageview. Called automatically when `autoPageviews` is on;
   * call it manually only if you disabled that, or for virtual pages.
   * Consecutive duplicate paths are ignored.
   * @param path Defaults to the current route: `location.pathname`, or the hash route / file name in desktop apps.
   */
  pageview(path?: string): void
  /**
   * Record a custom named event.
   * @param name Event name, e.g. `"signup"` (max 128 chars).
   * @param props Optional flat props, e.g. `{ plan: "pro" }`.
   */
  track<K extends keyof E & string>(name: K, ...props: TrackArgs<E[K]>): void
  /** Stop auto tracking and remove all listeners. */
  destroy(): void
}

const noop = () => {}
const NOOP: Analytics<any> = { pageview: noop, track: noop, destroy: noop }

/**
 * Create an analytics client. Prefer one instance per app (or use {@link init}).
 * Safe to call during SSR: returns a no-op client when `window` is undefined.
 *
 * @typeParam E Optional {@link EventMap} for typed `track()` calls.
 */
export function createAnalytics<E extends EventMap = AnyEvents>(options: AnalyticsOptions): Analytics<E> {
  if (typeof window === 'undefined') return NOOP
  const {
    endpoint,
    site,
    autoPageviews = true,
    clickAttribute = 'data-track',
    ignoreLocalhost = true,
    enabled = true,
    respectDnt = false,
    hashRoutes = true,
    version,
    beforeSend,
    debug,
  } = options
  const url = endpoint.replace(/\/+$/, '') + '/e'
  const nav = navigator as Navigator & { globalPrivacyControl?: boolean }
  const env = environment()
  const off =
    !enabled ||
    (ignoreLocalhost && env.dev) ||
    (respectDnt && (nav.doNotTrack === '1' || nav.globalPrivacyControl === true))
  // Read on every send, so ignoreThisBrowser() (e.g. an opt-out toggle) applies immediately.
  const isDisabled = () => off || safeGet(IGNORE_KEY) === '1'

  /** Route of the current page, never leaking query strings or local install paths. */
  const currentPath = () => {
    let p = location.pathname
    if (env.app) {
      // file:///C:/Users/me/app/resources/app.asar/dist/index.html → /  (settings.html → /settings.html)
      if (location.protocol === 'file:') p = p.slice(p.lastIndexOf('/'))
      p = p.replace(/\/index\.html?$/, '/') || '/'
    }
    const route = hashRoutes && /^#!?\//.test(location.hash) ? location.hash.replace(/^#!?/, '').split('?')[0]! : ''
    return !route ? p : p === '/' ? route : p + '#' + route
  }

  let lastPath: string | undefined
  let firstView = true

  const send = (p: Payload) => {
    const out = beforeSend ? beforeSend(p) : p
    if (out === false) return
    const disabled = isDisabled()
    if (debug) console.log('[minalytics]', disabled ? '(disabled)' : '', out)
    if (disabled) return
    const q = new URLSearchParams({ s: site, t: out.type[0]!, p: out.path, w: String(screen.width) })
    if (version) q.set('v', version)
    if (out.name) q.set('n', out.name)
    if (out.referrer) q.set('r', out.referrer)
    if (out.props) {
      const d = new URLSearchParams()
      for (const k in out.props) {
        const v = out.props[k]
        if (v != null) d.set(k, String(v))
      }
      if ([...d].length) q.set('d', d.toString())
    }
    const body = q.toString()
    // sendBeacon posts text/plain: a "simple" request, so no CORS preflight and survives page unload.
    if (!nav.sendBeacon?.(url, body)) {
      fetch(url, { method: 'POST', body, keepalive: true, mode: 'no-cors' }).catch(noop)
    }
  }

  const pageview = (path = currentPath()) => {
    if (path === lastPath) return
    lastPath = path
    let referrer: string | undefined
    if (firstView) {
      firstView = false
      const utm = new URLSearchParams(location.search).get('utm_source') ?? new URLSearchParams(location.search).get('ref')
      const ref = document.referrer
      referrer = utm || (ref && !ref.startsWith(location.origin) ? ref : undefined)
    }
    send({ type: 'pageview', path, referrer })
  }

  const track = (name: string, props?: Props) => send({ type: 'event', path: currentPath(), name, props })

  const cleanup: Array<() => void> = []
  const on = <K extends keyof WindowEventMap>(t: EventTarget, ev: K | string, fn: (e: any) => void, opts?: AddEventListenerOptions) => {
    t.addEventListener(ev, fn, opts)
    cleanup.push(() => t.removeEventListener(ev, fn, opts))
  }

  if (autoPageviews) {
    // Run after the router has updated location (pushState is synchronous, but frameworks may update title etc. after).
    const onNav = () => setTimeout(() => pageview(), 0)
    const origPush = history.pushState
    history.pushState = function (this: History, ...args: Parameters<History['pushState']>) {
      origPush.apply(this, args)
      onNav()
    }
    cleanup.push(() => {
      history.pushState = origPush
    })
    on(window, 'popstate', onNav)
    // Hash routers driven by plain links (<a href="#/settings">) only fire hashchange. Duplicates are deduped.
    if (hashRoutes) on(window, 'hashchange', onNav)
    // Don't count pages prerendered by the browser until actually shown.
    const doc = document as Document & { prerendering?: boolean }
    if (doc.prerendering) on(document, 'prerenderingchange', () => pageview(), { once: true })
    else pageview()
  }

  if (clickAttribute) {
    const prefix = clickAttribute + '-'
    on(document, 'click', (e: MouseEvent) => {
      const el = (e.target as Element | null)?.closest?.(`[${clickAttribute}]`)
      if (!el) return
      const props: Props = {}
      for (const a of el.attributes) if (a.name.startsWith(prefix)) props[a.name.slice(prefix.length)] = a.value
      track(el.getAttribute(clickAttribute)!, props)
    }, { capture: true })
  }

  return {
    pageview,
    track: track as Analytics<E>['track'],
    destroy: () => cleanup.splice(0).forEach((f) => f()),
  }
}

const IGNORE_KEY = 'minalytics:ignore'

/**
 * Where the page runs.
 * - `app`: loaded from `file:` or a custom scheme (`app://`, `tauri://`, `capacitor://`), i.e. a desktop/mobile app shell.
 * - `dev`: a development host, which `ignoreLocalhost` skips.
 */
function environment(): { app: boolean; dev: boolean } {
  const { protocol, hostname } = location
  if (/^https?:$/.test(protocol)) return { app: false, dev: /^(localhost|127\.|\[::1\]$|.*\.local$)|^$/.test(hostname) }
  // file: is production only inside Electron (a packaged app); elsewhere it's a local file opened in a browser.
  if (protocol === 'file:') return { app: true, dev: !/\bElectron\//.test(navigator.userAgent) }
  // Custom app schemes have a host (app://bundle, tauri://localhost); about:, data:, blob: don't and are skipped.
  return { app: !!hostname, dev: !hostname }
}

function safeGet(k: string): string | null {
  try {
    return localStorage.getItem(k)
  } catch {
    return null
  }
}

let instance: Analytics<any> = NOOP

/**
 * Initialize the shared (singleton) client used by {@link track} and {@link pageview}.
 * Call once at app startup, in the browser. Calling again replaces the previous instance.
 *
 * - **Vanilla / TS:** call in your entry module.
 * - **React:** call in `main.tsx` (outside components), or in a top-level `useEffect`.
 * - **Solid:** call in `index.tsx`, or in `onMount` of the root component.
 * - **Astro:** call in a `<script>` in your base layout (bundled, runs once per load; view transitions are tracked).
 */
export function init<E extends EventMap = AnyEvents>(options: AnalyticsOptions): Analytics<E> {
  instance.destroy()
  return (instance = createAnalytics<E>(options))
}

/** Track a custom event on the shared client. No-op before {@link init} or on the server. */
export function track(name: string, props?: Props): void {
  instance.track(name, props)
}

/** Track a pageview on the shared client. Usually automatic. No-op before {@link init} or on the server. */
export function pageview(path?: string): void {
  instance.pageview(path)
}

/**
 * Exclude (or re-include) the current browser from tracking, e.g. for yourself, or as the backing store for a
 * "send anonymous usage statistics" toggle in a desktop app. Persists in `localStorage` and takes effect immediately
 * for every client on this origin.
 * Tip: run `localStorage.setItem('minalytics:ignore', '1')` in the devtools console of your site.
 */
export function ignoreThisBrowser(ignore = true): void {
  try {
    ignore ? localStorage.setItem(IGNORE_KEY, '1') : localStorage.removeItem(IGNORE_KEY)
  } catch {}
}

/** Whether this browser is excluded via {@link ignoreThisBrowser}. Handy to render an opt-out toggle's state. */
export function isBrowserIgnored(): boolean {
  return safeGet(IGNORE_KEY) === '1'
}
