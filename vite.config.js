import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: './src/test/jsdom-environment.js',
    globals: true,
    setupFiles: './src/test/setup.js',
    css: false,
  },
})
