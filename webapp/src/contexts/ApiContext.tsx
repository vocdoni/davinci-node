import { createContext, useContext, useState, ReactNode } from 'react'

interface ApiContextType {
  apiUrl: string
  setApiUrl: (url: string) => void
}

const ApiContext = createContext<ApiContextType | undefined>(undefined)

// Function to get API URL from various sources
const getInitialApiUrl = (): string => {
  // 1. Check runtime config (injected by Docker)
  if (window.__RUNTIME_CONFIG__?.SEQUENCER_API_URL) {
    return window.__RUNTIME_CONFIG__.SEQUENCER_API_URL
  }

  // 2. Check build-time env var
  if (import.meta.env.SEQUENCER_API_URL) {
    return import.meta.env.SEQUENCER_API_URL
  }

  // 3. Default fallback
  return 'http://localhost:9090'
}

export const ApiProvider = ({ children }: { children: ReactNode }) => {
  const [apiUrl, setApiUrl] = useState(getInitialApiUrl())

  return (
    <ApiContext.Provider value={{ apiUrl, setApiUrl }}>
      {children}
    </ApiContext.Provider>
  )
}

export const useApi = () => {
  const context = useContext(ApiContext)
  if (!context) {
    throw new Error('useApi must be used within ApiProvider')
  }
  return context
}
