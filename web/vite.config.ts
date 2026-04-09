import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

const gatewayTarget = process.env.VITE_GATEWAY_TARGET || 'http://127.0.0.1:8080'
const controlPlaneTarget = process.env.VITE_CONTROL_PLANE_TARGET || 'http://127.0.0.1:8181'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    allowedHosts: 'all',
    proxy: {
      '/v1': {
        target: gatewayTarget,
        changeOrigin: true
      },
      '/api/v1/auth': {
        target: controlPlaneTarget,
        changeOrigin: true
      },
      '/api': {
        target: controlPlaneTarget,
        changeOrigin: true
      },
      '/auth': {
        target: controlPlaneTarget,
        changeOrigin: true,
        bypass(req) {
          if (req.url?.startsWith('/auth/callback') && req.method === 'GET') {
            return '/index.html'
          }
        }
      }
    }
  }
})