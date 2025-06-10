import { createContext, useContext, useState, ReactNode } from 'react'

interface ApiContextType {
  apiUrl: string
  setApiUrl: (url: string) => void
}

const ApiContext = createContext<ApiContextType | undefined>(undefined)

export const ApiProvider = ({ children }: { children: ReactNode }) => {
  const [apiUrl, setApiUrl] = useState(
    import.meta.env.SEQUENCER_API_URL || 'http://localhost:9090'
  )

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
