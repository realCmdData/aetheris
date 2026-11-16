import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The build lands in gateway/public, which Vercel serves from its CDN next to the Go gateway.
// It is committed: rebuild (pnpm build) before pushing UI changes.
export default defineConfig({
  plugins: [svelte()],
  build: { outDir: '../gateway/public', emptyOutDir: true, target: 'es2022' },
  // Local development: the NAS server runs on the PC with `-dev 127.0.0.1:8080`.
  server: { proxy: { '/api': 'http://127.0.0.1:8080' }, fs: { allow: ['.', '../assets'] } }, // the logo lives in assets/brand
})
