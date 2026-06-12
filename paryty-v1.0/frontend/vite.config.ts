import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  // Load env from frontend/.env (or parent) — VITE_API_URL / VITE_WS_URL
  // default to 127.0.0.1:8080 matching the Go query service default port.
  const apiTarget = process.env.VITE_API_URL || 'http://127.0.0.1:8080';
  const wsTarget = process.env.VITE_WS_URL || 'ws://127.0.0.1:8080';

  return {
    plugins: [react()],
    server: {
      host: '0.0.0.0',
      port: 3000,
      headers: {
        'Cross-Origin-Opener-Policy': 'same-origin',
        'Cross-Origin-Embedder-Policy': 'require-corp',
      },
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
        },
        '/ws': {
          target: wsTarget,
          ws: true,
        },
      },
    },
    build: {
      outDir: 'dist',
      sourcemap: true,
    },
    worker: {
      format: 'es',
    },
  };
})
