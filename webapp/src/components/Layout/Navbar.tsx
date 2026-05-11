import { Box, Container, Flex, Heading, HStack, Text, Link, Skeleton, Badge, Tooltip } from '@chakra-ui/react'
import { FaDatabase, FaExternalLinkAlt } from 'react-icons/fa'
import { useSequencerInfo } from '~hooks/useSequencerAPI'

export const Navbar = () => {
  const { data: info, isLoading } = useSequencerInfo()
  const networks = Object.entries(info?.networks ?? {})
  
  // Parse block explorer URLs from chainID-prefixed format: "11155111:https://...,1:https://..."
  const getBlockExplorerUrl = (chainId: string): string => {
    // Sources of the raw config (tried in order)
    const raw = window.__RUNTIME_CONFIG__?.BLOCK_EXPLORER_URL || import.meta.env.BLOCK_EXPLORER_URL || ''
    if (!raw) return 'https://sepolia.etherscan.io/address'

    const parts = raw.split(',')
    for (const part of parts) {
      const trimmedPart = part.trim()
      if (!trimmedPart) continue

      const [id, ...rest] = trimmedPart.split(':')
      const trimmedId = id.trim()
      const url = rest.join(':').trim()

      if (trimmedId === chainId && url) {
        return url
      }
    }

    return 'https://sepolia.etherscan.io/address'
  }

  const shortAddr = (addr: string) => `${addr.slice(0, 6)}...${addr.slice(-4)}`

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
                <Skeleton height="20px" width="100px" />
              ) : (
                <>
                  {networks.length === 0 ? (
                    <Text color="gray.400" fontSize="xs">N/A</Text>
                  ) : (
                    networks.map(([chainId, network]) => {
                      const addr = network.processRegistryContract

                      return (
                        <Tooltip key={chainId} label={addr || 'Not available'}>
                          <HStack spacing={1}>
                            <Text>{network.shortName}:</Text>
                            {addr ? (
                              <Link
                                href={`${getBlockExplorerUrl(chainId)}/${addr}`}
                                isExternal
                                display="inline-flex"
                                alignItems="center"
                                gap={1}
                                color="purple.500"
                                _hover={{ color: 'purple.600' }}
                              >
                                <Badge colorScheme="purple" fontSize="xs" fontFamily="mono">
                                  {shortAddr(addr)}
                                </Badge>
                                <FaExternalLinkAlt size={10} />
                              </Link>
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
