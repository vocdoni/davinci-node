/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly SEQUENCER_API_URL: string
  readonly BLOCK_EXPLORER_URL: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
