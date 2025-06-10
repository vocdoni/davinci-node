/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly SEQUENCER_API_URL: string
  readonly BLOCK_EXPLORER_URL: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

// Runtime configuration injected by Docker
interface RuntimeConfig {
  SEQUENCER_API_URL?: string
  BLOCK_EXPLORER_URL?: string
}

declare global {
  interface Window {
    __RUNTIME_CONFIG__?: RuntimeConfig
  }
}
