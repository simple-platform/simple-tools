import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    coverage: {
      exclude: ['dist/**', 'tests/**'],
      include: ['src/**'],
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
    },
    environment: 'happy-dom',
    include: ['tests/**/*.test.tsx', 'tests/**/*.test.ts'],
  },
})
