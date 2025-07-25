import { useQuery, UseQueryOptions } from '@tanstack/react-query'
import { useApi } from '~contexts/ApiContext'
import { InfoResponse, Process, ProcessListResponse, SequencerStatsResponse, WorkersResponse } from '~types/api'

// Helper function to handle API errors
const handleApiError = async (response: Response) => {
  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Unknown error' }))
    throw new Error(error.error || `HTTP error! status: ${response.status}`)
  }
  return response.json()
}

// Fetch sequencer info
export const useSequencerInfo = (options?: Omit<UseQueryOptions<InfoResponse>, 'queryKey' | 'queryFn'>) => {
  const { apiUrl } = useApi()
  
  return useQuery<InfoResponse>({
    queryKey: ['sequencer-info', apiUrl],
    queryFn: async () => {
      const response = await fetch(`${apiUrl}/info`)
      return handleApiError(response)
    },
    ...options,
  })
}

// Fetch process list
export const useProcessList = (options?: Omit<UseQueryOptions<ProcessListResponse>, 'queryKey' | 'queryFn'>) => {
  const { apiUrl } = useApi()
  
  return useQuery<ProcessListResponse>({
    queryKey: ['process-list', apiUrl],
    queryFn: async () => {
      const response = await fetch(`${apiUrl}/processes`)
      return handleApiError(response)
    },
    ...options,
  })
}

// Fetch individual process details
export const useProcess = (
  processId: string,
  options?: Omit<UseQueryOptions<Process>, 'queryKey' | 'queryFn'>
) => {
  const { apiUrl } = useApi()
  
  return useQuery<Process>({
    queryKey: ['process', processId, apiUrl],
    queryFn: async () => {
      const response = await fetch(`${apiUrl}/processes/${processId}`)
      return handleApiError(response)
    },
    enabled: !!processId,
    ...options,
  })
}

// Fetch multiple processes
export const useProcesses = (
  processIds: string[],
  options?: Omit<UseQueryOptions<Process[]>, 'queryKey' | 'queryFn'>
) => {
  const { apiUrl } = useApi()
  
  return useQuery<Process[]>({
    queryKey: ['processes', processIds, apiUrl],
    queryFn: async () => {
      const promises = processIds.map(id =>
        fetch(`${apiUrl}/processes/${id}`)
          .then(handleApiError)
          .catch(error => {
            console.warn(`Failed to fetch process ${id}:`, error.message)
            return null // Return null for failed processes
          })
      )
      const results = await Promise.all(promises)
      // Filter out null values (failed fetches)
      return results.filter((process): process is Process => process !== null)
    },
    enabled: processIds.length > 0,
    ...options,
  })
}

// Fetch sequencer statistics
export const useSequencerStats = (options?: Omit<UseQueryOptions<SequencerStatsResponse>, 'queryKey' | 'queryFn'>) => {
  const { apiUrl } = useApi()
  
  return useQuery<SequencerStatsResponse>({
    queryKey: ['sequencer-stats', apiUrl],
    queryFn: async () => {
      const response = await fetch(`${apiUrl}/sequencer/stats`)
      return handleApiError(response)
    },
    ...options,
  })
}

// Fetch workers list
export const useWorkers = (options?: Omit<UseQueryOptions<WorkersResponse>, 'queryKey' | 'queryFn'>) => {
  const { apiUrl } = useApi()
  
  return useQuery<WorkersResponse>({
    queryKey: ['workers', apiUrl],
    queryFn: async () => {
      const response = await fetch(`${apiUrl}/sequencer/workers`)
      return handleApiError(response)
    },
    ...options,
  })
}
