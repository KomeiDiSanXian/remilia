import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = dirname(__filename)

const host = process.env.TAURI_DEV_HOST

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@dashboard': resolve(__dirname, '../dashboard/src'),
      // 强制统一 React 实例，避免 @dashboard 组件使用 dashboard 的 node_modules/react
      'react': resolve(__dirname, 'node_modules/react'),
      'react-dom': resolve(__dirname, 'node_modules/react-dom'),
    },
  },
  build: {
    outDir: 'dist',
  },
  server: {
    port: 1420,
    strictPort: true,
    host: host || false,
    hmr: host
      ? { protocol: 'ws', host, port: 1421 }
      : undefined,
    proxy: {
      '/api': {
        target: 'http://localhost:9002',
        changeOrigin: true,
      },
    },
  },
})
