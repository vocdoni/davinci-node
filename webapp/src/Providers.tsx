import { ChakraProvider } from '@chakra-ui/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { ApiProvider } from './contexts/ApiContext'
import { Router } from './router'
import { theme } from './themes/main'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchInterval: 10000, // 10 seconds
      staleTime: 5000, // 5 seconds
      retry: 2,
    },
  },
})

const Providers = () => (
  <ApiProvider>
    <QueryClientProvider client={queryClient}>
      <ReactQueryDevtools initialIsOpen={false} />
      <ChakraProvider theme={theme}>
        <Router />
      </ChakraProvider>
    </QueryClientProvider>
  </ApiProvider>
)

export default Providers
