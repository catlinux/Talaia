import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitest/config'

// The application version lives in the repository VERSION file; it is exposed
// to the code as the __APP_VERSION__ constant (see src/vite-env.d.ts).
const version = readFileSync(fileURLToPath(new URL('../VERSION', import.meta.url)), 'utf8').trim()

export default defineConfig({
  plugins: [react()],
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  server: {
    proxy: {
      // The core runs in insecure_dev mode on this address. changeOrigin stays
      // false so the browser's Origin (http://localhost:5173) reaches the core,
      // which allows it in that mode.
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
  test: {
    environment: 'jsdom',
  },
})
