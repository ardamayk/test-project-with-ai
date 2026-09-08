import { defineConfig } from 'vitest/config';

export default defineConfig({
  cacheDir: process.env.EARTHLY_VITE_CACHE
    ? `${process.env.EARTHLY_VITE_CACHE}/api-client-unit`
    : undefined,
  test: {
    coverage: {
      reportsDirectory: process.env.EARTHLY_RUN_DIR
        ? `${process.env.EARTHLY_RUN_DIR}/api-client-unit/coverage`
        : 'coverage',
    },
    include: ['src/**/*.test.ts'],
  },
});
