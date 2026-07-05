import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// During `npm run dev`, Vite serves the UI on :5173 and proxies the data/config
// APIs and uploaded backgrounds to the Go server on :7171. The production build
// is emitted to `dist/`, which the Go binary embeds and serves itself.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:7171',
      '/backgrounds': 'http://127.0.0.1:7171',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
