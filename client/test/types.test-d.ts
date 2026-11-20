import { expectTypeOf, test } from 'vitest'
import { createAnalytics } from '../src/index'

type Events = {
  signup: { plan: 'free' | 'pro' }
  newsletter: { source?: string }
  logout: void
}

test('typed events', () => {
  const a = createAnalytics<Events>({ endpoint: 'x', site: 'y' })
  a.track('signup', { plan: 'pro' })
  a.track('newsletter')
  a.track('newsletter', { source: 'footer' })
  a.track('logout')
  // @ts-expect-error unknown event
  a.track('nope')
  // @ts-expect-error props required
  a.track('signup')
  // @ts-expect-error invalid prop value
  a.track('signup', { plan: 'enterprise' })
  expectTypeOf(a.pageview).parameter(0).toEqualTypeOf<string | undefined>()
})

test('untyped client accepts any event', () => {
  const a = createAnalytics({ endpoint: 'x', site: 'y' })
  a.track('anything')
  a.track('anything', { n: 1, b: true, s: 's' })
  // @ts-expect-error nested objects are not allowed
  a.track('x', { nested: { a: 1 } })
})
