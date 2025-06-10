import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'
import tsconfigPaths from 'vite-tsconfig-paths'

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  // Load env variables from .env files
  const env = loadEnv(mode, process.cwd(), '')
  
  return {
    base: '/',
    build: {
      outDir: 'dist',
    },
    define: {
      'import.meta.env.SEQUENCER_API_URL': JSON.stringify(env.SEQUENCER_API_URL || 'http://localhost:9090'),
      'import.meta.env.BLOCK_EXPLORER_URL': JSON.stringify(env.BLOCK_EXPLORER_URL || 'https://sepolia.etherscan.io/address'),
    },
    plugins: [
      tsconfigPaths(),
      react(),
    ],
    server: {
      port: 3000,
      cors: true,
    },
  }
})
