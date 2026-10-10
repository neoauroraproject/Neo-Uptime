import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import fs from 'node:fs'
import path from 'node:path'

const backend = process.env.NEO_BACKEND ?? 'http://127.0.0.1:7788'

// Country flags come from the flag-icons package (MIT). Rather than copying
// 250 files into the repository, they are served from node_modules in dev and
// written to dist/flags at build time.
function flags(): Plugin {
  const src = path.resolve(__dirname, 'node_modules/flag-icons/flags/4x3')
  return {
    name: 'neo-flags',
    configureServer(server) {
      server.middlewares.use('/flags', (req, res, next) => {
        const file = path.join(src, path.basename((req.url ?? '').split('?')[0]))
        if (file.endsWith('.svg') && fs.existsSync(file)) {
          res.setHeader('Content-Type', 'image/svg+xml')
          fs.createReadStream(file).pipe(res)
          return
        }
        next()
      })
    },
    generateBundle() {
      for (const f of fs.readdirSync(src)) {
        if (/^[a-z]{2}\.svg$/.test(f)) {
          this.emitFile({ type: 'asset', fileName: `flags/${f}`, source: fs.readFileSync(path.join(src, f)) })
        }
      }
      this.emitFile({ type: 'asset', fileName: 'flags/LICENSE', source: fs.readFileSync(path.resolve(__dirname, 'node_modules/flag-icons/LICENSE')) })
    },
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss(), flags()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: backend, changeOrigin: false },
      '/metrics': { target: backend },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
})
