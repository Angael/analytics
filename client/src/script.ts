/**
 * Drop-in `<script>` build. Configure with data attributes:
 *
 * ```html
 * <script defer src="https://stats.example.com/a.js" data-site="blog"></script>
 * ```
 *
 * `data-endpoint` defaults to the script's origin. Optional: `data-auto="false"`, `data-dnt="true"`.
 * Exposes `window.minalytics` with `track(name, props)` and `pageview(path)`.
 */
import { init, track, pageview } from './index'

const s = document.currentScript as HTMLScriptElement | null
if (s) {
  const d = s.dataset
  init({
    endpoint: d.endpoint || new URL(s.src).origin,
    site: d.site || location.hostname,
    autoPageviews: d.auto !== 'false',
    respectDnt: d.dnt === 'true',
  })
}
;(window as any).minalytics = { track, pageview }
