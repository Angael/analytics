// @vitest-environment happy-dom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAnalytics, init, track, pageview, ignoreThisBrowser, isBrowserIgnored, type Analytics } from '../src/index'

const sent: URLSearchParams[] = []
let client: Analytics | undefined

const tick = () => new Promise((r) => setTimeout(r, 5))
const make = (opts: Partial<Parameters<typeof createAnalytics>[0]> = {}) =>
  (client = createAnalytics({ endpoint: 'https://pulse.example.com/', site: 'blog', ignoreLocalhost: false, ...opts }))

const BASE = location.href
const ELECTRON_UA =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) my-app/1.4.0 Chrome/130.0.0.0 Electron/33.2.1 Safari/537.36'
const realUA = navigator.userAgent
const setUA = (ua: string) => Object.defineProperty(navigator, 'userAgent', { value: ua, configurable: true })
const goto = (url: string) => (window as any).happyDOM.setURL(url)

beforeEach(() => {
  sent.length = 0
  goto(BASE)
  setUA(realUA)
  localStorage.clear()
  history.replaceState(null, '', '/')
  Object.defineProperty(document, 'referrer', { value: '', configurable: true })
  ;(navigator as any).sendBeacon = vi.fn((url: string, body: string) => {
    expect(url).toBe('https://pulse.example.com/e')
    sent.push(new URLSearchParams(body))
    return true
  })
})
afterEach(() => {
  client?.destroy()
  client = undefined
})

describe('pageviews', () => {
  it('sends the initial pageview with site, path and width', () => {
    history.replaceState(null, '', '/hello?secret=1#x')
    make()
    expect(sent).toHaveLength(1)
    const q = sent[0]!
    expect(q.get('s')).toBe('blog')
    expect(q.get('t')).toBe('p')
    expect(q.get('p')).toBe('/hello') // no query, no hash
    expect(q.get('w')).toMatch(/^\d+$/)
  })

  it('tracks pushState and popstate navigations, deduping same path', async () => {
    make()
    history.pushState(null, '', '/a')
    await tick()
    history.pushState(null, '', '/a')
    await tick()
    history.pushState(null, '', '/b')
    await tick()
    dispatchEvent(new PopStateEvent('popstate'))
    history.replaceState(null, '', '/a')
    dispatchEvent(new PopStateEvent('popstate'))
    await tick()
    expect(sent.map((q) => q.get('p'))).toEqual(['/', '/a', '/b', '/a'])
  })

  it('sends external referrer only on the first pageview', async () => {
    Object.defineProperty(document, 'referrer', { value: 'https://news.ycombinator.com/item', configurable: true })
    make()
    history.pushState(null, '', '/next')
    await tick()
    expect(sent[0]!.get('r')).toBe('https://news.ycombinator.com/item')
    expect(sent[1]!.has('r')).toBe(false)
  })

  it('prefers utm_source over referrer, ignores same-origin referrer', () => {
    Object.defineProperty(document, 'referrer', { value: location.origin + '/x', configurable: true })
    make()
    expect(sent[0]!.has('r')).toBe(false)
    client!.destroy()
    sent.length = 0
    history.replaceState(null, '', '/?utm_source=newsletter')
    make()
    expect(sent[0]!.get('r')).toBe('newsletter')
  })

  it('autoPageviews: false sends nothing until called', () => {
    make({ autoPageviews: false })
    expect(sent).toHaveLength(0)
    client!.pageview('/virtual')
    expect(sent[0]!.get('p')).toBe('/virtual')
  })

  it('destroy restores history.pushState', async () => {
    const orig = history.pushState
    make()
    expect(history.pushState).not.toBe(orig)
    client!.destroy()
    expect(history.pushState).toBe(orig)
    history.pushState(null, '', '/after')
    await tick()
    expect(sent).toHaveLength(1)
  })
})

describe('events', () => {
  it('sends name and url-encoded props, dropping null/undefined', () => {
    make({ autoPageviews: false })
    client!.track('signup', { plan: 'pro', n: 2, ok: true, gone: undefined, nil: null })
    const q = sent[0]!
    expect(q.get('t')).toBe('e')
    expect(q.get('n')).toBe('signup')
    expect(Object.fromEntries(new URLSearchParams(q.get('d')!))).toEqual({ plan: 'pro', n: '2', ok: 'true' })
  })

  it('omits d when there are no props', () => {
    make({ autoPageviews: false })
    client!.track('x')
    client!.track('y', { a: undefined })
    expect(sent.every((q) => !q.has('d'))).toBe(true)
  })

  it('tracks clicks on [data-track] elements with data-track-* props', () => {
    make({ autoPageviews: false })
    document.body.innerHTML = '<button data-track="cta" data-track-plan="pro"><span id="inner">Go</span></button>'
    document.getElementById('inner')!.click()
    expect(sent[0]!.get('n')).toBe('cta')
    expect(new URLSearchParams(sent[0]!.get('d')!).get('plan')).toBe('pro')
  })

  it('clickAttribute: false disables click tracking', () => {
    make({ autoPageviews: false, clickAttribute: false })
    document.body.innerHTML = '<button data-track="cta">Go</button>'
    document.querySelector('button')!.click()
    expect(sent).toHaveLength(0)
  })
})

describe('filters', () => {
  it('beforeSend can rewrite or drop', () => {
    make({
      autoPageviews: false,
      beforeSend: (p) => (p.name === 'drop' ? false : { ...p, path: p.path.replace(/\d+/g, ':id') }),
    })
    history.replaceState(null, '', '/user/123')
    client!.track('drop')
    client!.track('keep')
    expect(sent).toHaveLength(1)
    expect(sent[0]!.get('p')).toBe('/user/:id')
  })

  it('ignores localhost by default', () => {
    make({ ignoreLocalhost: true })
    expect(location.hostname).toBe('localhost')
    expect(sent).toHaveLength(0)
  })

  it('respects DNT / GPC when enabled', () => {
    ;(navigator as any).globalPrivacyControl = true
    make({ respectDnt: true })
    expect(sent).toHaveLength(0)
    delete (navigator as any).globalPrivacyControl
  })

  it('ignoreThisBrowser opts out via localStorage, effective immediately', () => {
    ignoreThisBrowser()
    expect(isBrowserIgnored()).toBe(true)
    make({ autoPageviews: false })
    client!.track('a')
    ignoreThisBrowser(false)
    expect(isBrowserIgnored()).toBe(false)
    client!.track('b')
    ignoreThisBrowser()
    client!.track('c')
    expect(sent.map((q) => q.get('n'))).toEqual(['b'])
  })

  it('enabled: false sends nothing', () => {
    make({ enabled: false })
    client!.track('x')
    expect(sent).toHaveLength(0)
  })

  it('falls back to fetch when sendBeacon is unavailable', () => {
    ;(navigator as any).sendBeacon = undefined
    const f = vi.fn(() => Promise.resolve(new Response()))
    vi.stubGlobal('fetch', f)
    make()
    expect(f).toHaveBeenCalledOnce()
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('https://pulse.example.com/e')
    expect(init).toMatchObject({ method: 'POST', keepalive: true })
    vi.unstubAllGlobals()
  })
})

describe('singleton', () => {
  it('is a no-op before init, works after, and re-init replaces', async () => {
    track('early')
    pageview('/early')
    expect(sent).toHaveLength(0)
    init({ endpoint: 'https://pulse.example.com', site: 'a', ignoreLocalhost: false })
    track('later')
    init({ endpoint: 'https://pulse.example.com', site: 'b', ignoreLocalhost: false })
    history.pushState(null, '', '/z')
    await tick()
    expect(sent.map((q) => q.get('s') + ':' + (q.get('n') ?? q.get('p')))).toEqual(['a:/', 'a:later', 'b:/', 'b:/z'])
    init({ endpoint: 'x', site: 'x', autoPageviews: false, ignoreLocalhost: true }).destroy()
  })
})

describe('version', () => {
  it('sends v on every request when set, omits it otherwise', () => {
    make({ version: '1.4.0' })
    client!.track('x')
    expect(sent.map((q) => q.get('v'))).toEqual(['1.4.0', '1.4.0'])
    client!.destroy()
    sent.length = 0
    make()
    expect(sent[0]!.has('v')).toBe(false)
  })
})

describe('hash routes', () => {
  it('tracks #/ routes as pages, via pushState and hashchange', async () => {
    history.replaceState(null, '', '/#/inbox?tab=2')
    make()
    history.pushState(null, '', '/#/settings')
    await tick()
    history.replaceState(null, '', '/#/about')
    dispatchEvent(new HashChangeEvent('hashchange'))
    await tick()
    client!.track('x')
    expect(sent.map((q) => q.get('p'))).toEqual(['/inbox', '/settings', '/about', '/about'])
  })

  it('supports #! and appends the route to non-root paths', () => {
    history.replaceState(null, '', '/docs#!/intro')
    make()
    expect(sent[0]!.get('p')).toBe('/docs#/intro')
  })

  it('ignores plain anchors and can be disabled', () => {
    history.replaceState(null, '', '/page#pricing')
    make()
    expect(sent[0]!.get('p')).toBe('/page')
    client!.destroy()
    sent.length = 0
    history.replaceState(null, '', '/#/settings')
    make({ hashRoutes: false })
    expect(sent[0]!.get('p')).toBe('/')
  })
})

describe('desktop apps', () => {
  const opts = { ignoreLocalhost: true } // the real default: these must work without opting in

  it('packaged Electron (file:) is tracked with clean routes, never the install path', () => {
    setUA(ELECTRON_UA)
    goto('file:///C:/Users/alice/AppData/Local/my-app/resources/app.asar/dist/index.html#/settings')
    make(opts)
    expect(sent).toHaveLength(1)
    expect(sent[0]!.get('p')).toBe('/settings')
    client!.track('x')
    expect(sent[1]!.get('p')).toBe('/settings')
  })

  it('Electron secondary windows report their file name', () => {
    setUA(ELECTRON_UA)
    goto('file:///opt/my-app/resources/app/dist/prefs.html')
    make(opts)
    expect(sent[0]!.get('p')).toBe('/prefs.html')
  })

  it('a plain browser opening a local file is still ignored', () => {
    goto('file:///home/alice/page.html')
    make(opts)
    expect(sent).toHaveLength(0)
  })

  it('Electron in dev mode (localhost dev server) is ignored', () => {
    setUA(ELECTRON_UA)
    make(opts)
    expect(sent).toHaveLength(0)
  })

  it('custom app schemes count as production, including tauri://localhost', () => {
    for (const u of ['app://bundle/index.html#/home', 'tauri://localhost/#/home', 'capacitor://localhost/home']) {
      goto(u)
      make(opts)
      client!.destroy()
    }
    expect(sent.map((q) => q.get('p'))).toEqual(['/home', '/home', '/home'])
  })
})
