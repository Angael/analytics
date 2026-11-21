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
export type PropValue = string | number | boolean | null | undefined;
/** Flat key/value properties attached to a custom event. Keep them short: they are stored as-is. */
export type Props = Record<string, PropValue>;
/**
 * Map of event name → props shape. Use `void` for events without props.
 * Pass it as a generic to {@link createAnalytics} to get autocompletion and type checking for {@link Analytics.track}.
 */
export type EventMap = {
    [name: string]: Props | void | undefined;
};
/** Default, untyped event map: any event name, optional props. */
export type AnyEvents = Record<string, Props | undefined>;
/** Resolves the rest-args of `track()`: props are optional when the shape allows `undefined`/`void` or has no required keys. */
export type TrackArgs<P> = undefined extends P ? [props?: Exclude<P, void>] : {} extends P ? [props?: P] : [props: P];
/** Options for {@link createAnalytics} / {@link init}. */
export interface AnalyticsOptions {
    /**
     * Base URL of your analytics server, e.g. `https://stats.example.com`.
     * Events are sent to `${endpoint}/e`.
     */
    endpoint: string;
    /** Site key that groups data on the dashboard, e.g. `"blog"`. Must be allowed by the server's `SITES` (if set). */
    site: string;
    /**
     * Automatically track the initial pageview and client-side navigations
     * (`history.pushState`, `popstate`). Works with React Router, Solid Router, Astro view transitions, etc.
     * @default true
     */
    autoPageviews?: boolean;
    /**
     * Track clicks on elements with this attribute as custom events.
     * `<button data-track="signup" data-track-plan="pro">` sends event `signup` with `{ plan: "pro" }`.
     * Set `false` to disable.
     * @default "data-track"
     */
    clickAttribute?: string | false;
    /**
     * Don't send anything from development hosts: `localhost`, `127.0.0.1`, `[::1]`, `*.local`, and `file:` pages
     * outside Electron. A packaged Electron app (loaded from `file:`) and custom app schemes (`app://`, `tauri://`)
     * count as production; an Electron app in dev mode (served from `localhost`) is ignored like any dev server.
     * @default true
     */
    ignoreLocalhost?: boolean;
    /**
     * Master switch. `false` sends nothing at all (events still reach `debug` logging).
     * Use it for an "anonymous usage statistics" setting in a desktop app, or to gate on your own build flags.
     * @default true
     */
    enabled?: boolean;
    /**
     * App or release version, e.g. `"1.4.0"` (max 32 chars). Shown as "App versions" on the dashboard, so you can see
     * which releases are in use. Most useful for desktop apps; in Electron pass `app.getVersion()` from the preload
     * script, or inject your `package.json` version at build time.
     */
    version?: string;
    /**
     * Treat hash routes (`#/settings`, `#!/settings`) as pages. Plain anchors like `#pricing` are never tracked.
     * The route replaces the path when the page itself is `/`, otherwise it is appended: `/docs#/intro`.
     * Hash routing is the norm in desktop apps loaded from `file:`.
     * @default true
     */
    hashRoutes?: boolean;
    /**
     * Don't send anything when the browser has Do Not Track / Global Privacy Control enabled.
     * @default false
     */
    respectDnt?: boolean;
    /**
     * Hook to rewrite or drop events before sending. Return `false` to drop.
     * Useful to strip IDs from paths: `p => ({ ...p, path: p.path.replace(/\/\d+/g, '/:id') })`.
     */
    beforeSend?: (payload: Payload) => Payload | false;
    /** Log events to the console instead of being silent. @default false */
    debug?: boolean;
}
/** The event about to be sent. Exposed to {@link AnalyticsOptions.beforeSend}. */
export interface Payload {
    /** `"pageview"` or `"event"`. */
    type: 'pageview' | 'event';
    /** Page path (no query string). Includes the hash route when {@link AnalyticsOptions.hashRoutes} applies. */
    path: string;
    /** Event name (custom events only). */
    name?: string;
    /** Referrer URL or `utm_source` (pageviews only, only for external referrers). */
    referrer?: string;
    /** Event props (custom events only). */
    props?: Props;
}
/** An analytics client. Create one with {@link createAnalytics}. */
export interface Analytics<E extends EventMap = AnyEvents> {
    /**
     * Record a pageview. Called automatically when `autoPageviews` is on;
     * call it manually only if you disabled that, or for virtual pages.
     * Consecutive duplicate paths are ignored.
     * @param path Defaults to the current route: `location.pathname`, or the hash route / file name in desktop apps.
     */
    pageview(path?: string): void;
    /**
     * Record a custom named event.
     * @param name Event name, e.g. `"signup"` (max 128 chars).
     * @param props Optional flat props, e.g. `{ plan: "pro" }`.
     */
    track<K extends keyof E & string>(name: K, ...props: TrackArgs<E[K]>): void;
    /** Stop auto tracking and remove all listeners. */
    destroy(): void;
}
/**
 * Create an analytics client. Prefer one instance per app (or use {@link init}).
 * Safe to call during SSR: returns a no-op client when `window` is undefined.
 *
 * @typeParam E Optional {@link EventMap} for typed `track()` calls.
 */
export declare function createAnalytics<E extends EventMap = AnyEvents>(options: AnalyticsOptions): Analytics<E>;
/**
 * Initialize the shared (singleton) client used by {@link track} and {@link pageview}.
 * Call once at app startup, in the browser. Calling again replaces the previous instance.
 *
 * - **Vanilla / TS:** call in your entry module.
 * - **React:** call in `main.tsx` (outside components), or in a top-level `useEffect`.
 * - **Solid:** call in `index.tsx`, or in `onMount` of the root component.
 * - **Astro:** call in a `<script>` in your base layout (bundled, runs once per load; view transitions are tracked).
 */
export declare function init<E extends EventMap = AnyEvents>(options: AnalyticsOptions): Analytics<E>;
/** Track a custom event on the shared client. No-op before {@link init} or on the server. */
export declare function track(name: string, props?: Props): void;
/** Track a pageview on the shared client. Usually automatic. No-op before {@link init} or on the server. */
export declare function pageview(path?: string): void;
/**
 * Exclude (or re-include) the current browser from tracking, e.g. for yourself, or as the backing store for a
 * "send anonymous usage statistics" toggle in a desktop app. Persists in `localStorage` and takes effect immediately
 * for every client on this origin.
 * Tip: run `localStorage.setItem('minalytics:ignore', '1')` in the devtools console of your site.
 */
export declare function ignoreThisBrowser(ignore?: boolean): void;
/** Whether this browser is excluded via {@link ignoreThisBrowser}. Handy to render an opt-out toggle's state. */
export declare function isBrowserIgnored(): boolean;
