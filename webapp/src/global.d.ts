declare global {
  interface Window {
    __RUNTIME_CONFIG__?: {
      SEQUENCER_API_URL?: string
      BLOCK_EXPLORER_URL?: string
    }
  }
}

export {}
