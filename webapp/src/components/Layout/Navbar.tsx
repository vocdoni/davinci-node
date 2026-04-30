import { Box, Container, Flex, Heading, HStack, Text, Link, Skeleton, Badge, Tooltip } from '@chakra-ui/react'
import { FaDatabase, FaExternalLinkAlt } from 'react-icons/fa'
import { useSequencerInfo } from '~hooks/useSequencerAPI'

export const Navbar = () => {
  const { data: info, isLoading } = useSequencerInfo()
  const runtimes = Object.entries(info?.runtimes ?? {})
  
  // Get block explorer URL from runtime config (injected by Docker) or fallback to build-time env var
  const getBlockExplorerUrl = (): string => {
    // 1. Check runtime config (injected by Docker)
    if (window.__RUNTIME_CONFIG__?.BLOCK_EXPLORER_URL) {
      return window.__RUNTIME_CONFIG__.BLOCK_EXPLORER_URL
    }
    
    // 2. Check build-time env var
    if (import.meta.env.BLOCK_EXPLORER_URL) {
      return import.meta.env.BLOCK_EXPLORER_URL
    }
    
    // 3. Default fallback
    return 'https://sepolia.etherscan.io/address'
  }
  
  const blockExplorerUrl = getBlockExplorerUrl()

  return (
    <Box bg="white" shadow="sm" borderBottom="1px" borderColor="gray.200">
      <Container maxW="container.xl">
        <Flex h={16} alignItems="center" justifyContent="space-between">
          <HStack spacing={6}>
            <HStack spacing={3}>
              <Box color="purple.500">
                <FaDatabase size={24} />
              </Box>
              <Heading size="md" color="gray.700">
                Davinci Sequencer Dashboard
              </Heading>
            </HStack>
            
            {/* Compact contract links */}
            <HStack spacing={4} fontSize="sm" color="gray.600">
              {isLoading ? (
                <>
                  <Skeleton height="20px" width="100px" />
                </>
              ) : (
                <>
                  {runtimes.length === 0 ? (
                    <Text color="gray.400" fontSize="xs">N/A</Text>
                  ) : (
                    runtimes.map(([chainId, runtime]) => {
                      const processAddress = runtime.contracts.process
                      const processLabel = `${runtime.network} (${chainId})`

                      return (
                        <Tooltip key={chainId} label={processAddress || 'Not available'}>
                          <HStack spacing={1}>
                            <Text>{processLabel}:</Text>
                            {processAddress ? (
                              runtimes.length === 1 ? (
                                <Link
                                  href={`${blockExplorerUrl}/${processAddress}`}
                                  isExternal
                                  display="inline-flex"
                                  alignItems="center"
                                  gap={1}
                                  color="purple.500"
                                  _hover={{ color: 'purple.600' }}
                                >
                                  <Badge colorScheme="purple" fontSize="xs" fontFamily="mono">
                                    {processAddress.slice(0, 6)}...{processAddress.slice(-4)}
                                  </Badge>
                                  <FaExternalLinkAlt size={10} />
                                </Link>
                              ) : (
                                <Badge colorScheme="purple" fontSize="xs" fontFamily="mono">
                                  {processAddress.slice(0, 6)}...{processAddress.slice(-4)}
                                </Badge>
                              )
                            ) : (
                              <Text color="gray.400" fontSize="xs">N/A</Text>
                            )}
                          </HStack>
                        </Tooltip>
                      )
                    })
                  )}
                </>
              )}
            </HStack>
          </HStack>
          
          <Text fontSize="sm" color="gray.500">
            Last refresh: {new Date().toLocaleTimeString()}
          </Text>
        </Flex>
      </Container>
    </Box>
  )
}
