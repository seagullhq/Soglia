import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Built assets and the dev server both serve the console at the site root.
// In dev, Vite proxies API calls to the Go server over HTTPS. The local CA is
// self-signed, so the proxy must skip verification (secure: false) — this is a
// development-only setting.
function readCert(name: string) {
  try {
    return readFileSync(fileURLToPath(new URL(`../certs/${name}`, import.meta.url)))
  } catch {
    return undefined
  }
}

const cert = readCert('api.crt')
const key = readCert('api.key')
const apiProxy = { target: 'https://127.0.0.1:8080', secure: false }

export default defineConfig({
  base: '/',
  plugins: [react()],
  server: {
    port: 5173,
    https: cert && key ? { cert, key } : undefined,
    proxy: {
      '/auth': apiProxy,
      '/health': apiProxy,
      '/config': apiProxy,
      '/stats': apiProxy,
      '/history': apiProxy,
      '/reviews': apiProxy,
      '/db': apiProxy,
      '/setup': apiProxy,
      '/users': apiProxy,
      '/files': apiProxy,
      '/terminal': apiProxy,
      '/moderate': apiProxy,
    },
  },
})
