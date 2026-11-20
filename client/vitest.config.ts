import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'happy-dom',
    typecheck: { enabled: true, include: ['test/**/*.test-d.ts'] },
  },
})
