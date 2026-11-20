// @vitest-environment node
import { expect, it } from 'vitest'
import { createAnalytics, init, track, pageview } from '../src/index'

it('is a silent no-op without window (SSR)', () => {
  const a = createAnalytics({ endpoint: 'https://x', site: 'y' })
  expect(() => {
    a.pageview()
    a.track('x', { a: 1 })
    a.destroy()
    init({ endpoint: 'https://x', site: 'y' })
    track('x')
    pageview('/')
  }).not.toThrow()
})
