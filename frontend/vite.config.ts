import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

export default defineConfig(({ mode }) => {
  const repoRoot = path.resolve(__dirname, '..')
  const env = loadEnv(mode, repoRoot, '')
  const frontendPort = Number.parseInt(env.VITE_PORT || env.FRONTEND_PORT || '3000', 10)
  const backendTarget = env.VITE_API_TARGET || env.BACKEND_URL || `http://localhost:${env.PORT || '8080'}`
  const allowedHosts = (env.VITE_ALLOWED_HOSTS || '')
    .split(',')
    .map((host) => host.trim())
    .filter(Boolean)

  return {
    plugins: [react(), tailwindcss()],
    root: path.resolve(__dirname),
    envDir: repoRoot,
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    server: {
      port: frontendPort,
      strictPort: true,
      allowedHosts,
      proxy: {
        '/api': {
          target: backendTarget,
          changeOrigin: true,
          ws: true,
          configure: (proxy) => {
            proxy.on('error', (_err, _req, res) => {
              if ('writeHead' in res && !('headersSent' in res && res.headersSent)) {
                const httpRes = res as import('http').ServerResponse;
                httpRes.writeHead(502, {
                  'Content-Type': 'application/json',
                  'X-Proxy-Error': 'true',
                });
                httpRes.end(JSON.stringify({ error: 'Backend is offline', proxy_error: true }));
              }
            })
          },
        },
      },
    },
  }
})
