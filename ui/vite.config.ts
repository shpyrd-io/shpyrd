import path from 'node:path'
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  server: {
    // `npm run dev` talks to a locally running shpyrd-server.
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    // Embedded into the server binary by ui/embed.go. Two applications
    // (RFC-0080): the console and the workspace one, each its own entry;
    // the server serves dist/apps/console/index.html at the console host
    // and dist/apps/workspace/index.html everywhere else. Chunks are
    // shared under dist/assets.
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        console: path.resolve(import.meta.dirname, 'apps/console/index.html'),
        workspace: path.resolve(import.meta.dirname, 'apps/workspace/index.html'),
      },
    },
  },
  test: {
    // Pure-logic unit tests (parsers, formatters); no DOM needed.
    environment: 'node',
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx'],
  },
})
