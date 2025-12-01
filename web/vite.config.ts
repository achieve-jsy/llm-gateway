import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'path'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src')
    }
  },
  server: {
    port: 5173,
    allowedHosts: ['aimeter.xk-devops.com'],
    https: {
      key: resolve(__dirname, 'certs/localhost.key'),
      cert: resolve(__dirname, 'certs/localhost.crt'),
    },
    proxy: {
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true
      },
      '/api/v1/auth': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/api': {
        target: 'http://localhost:8081',
        changeOrigin: true
      },
      '/auth': {
        target: 'http://localhost:8081',
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