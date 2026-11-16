import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The Windows app's window: same design system, built into client-win/ui, which WebView2 serves from next to the exe.
// `pnpm dev:desktop` shows it in a browser with sample data (bridge.ts mocks the app).
export default defineConfig({
  root: 'desktop',
  base: './',
  publicDir: false,
  plugins: [svelte()],
  build: { outDir: '../../client-win/ui', emptyOutDir: true, target: 'es2022' },
  server: { port: 5174, strictPort: true, fs: { allow: ['..', '../../assets'] } },
})
