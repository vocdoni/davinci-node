import { Box, Container, Flex, Heading, HStack, Text, Link, Skeleton, Badge, Tooltip } from '@chakra-ui/react'
import { FaDatabase, FaExternalLinkAlt } from 'react-icons/fa'
import { useSequencerInfo } from '~hooks/useSequencerAPI'

export const Navbar = () => {
  const { data: info, isLoading } = useSequencerInfo()
  const blockExplorerUrl = import.meta.env.BLOCK_EXPLORER_URL || 'https://sepolia.etherscan.io/address'

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
                  <Skeleton height="20px" width="100px" />
                </>
              ) : (
                <>
                  <Tooltip label={info?.contracts?.process || 'Not available'}>
                    <HStack spacing={1}>
                      <Text>Process:</Text>
                      {info?.contracts?.process ? (
                        <Link
                          href={`${blockExplorerUrl}/${info.contracts.process}`}
                          isExternal
                          display="inline-flex"
                          alignItems="center"
                          gap={1}
                          color="purple.500"
                          _hover={{ color: 'purple.600' }}
                        >
                          <Badge colorScheme="purple" fontSize="xs" fontFamily="mono">
                            {info.contracts.process.slice(0, 6)}...{info.contracts.process.slice(-4)}
                          </Badge>
                          <FaExternalLinkAlt size={10} />
                        </Link>
                      ) : (
                        <Text color="gray.400" fontSize="xs">N/A</Text>
                      )}
                    </HStack>
                  </Tooltip>
                  
                  <Tooltip label={info?.contracts?.organization || 'Not available'}>
                    <HStack spacing={1}>
                      <Text>Org:</Text>
                      {info?.contracts?.organization ? (
                        <Link
                          href={`${blockExplorerUrl}/${info.contracts.organization}`}
                          isExternal
                          display="inline-flex"
                          alignItems="center"
                          gap={1}
                          color="purple.500"
                          _hover={{ color: 'purple.600' }}
                        >
                          <Badge colorScheme="purple" fontSize="xs" fontFamily="mono">
                            {info.contracts.organization.slice(0, 6)}...{info.contracts.organization.slice(-4)}
                          </Badge>
                          <FaExternalLinkAlt size={10} />
                        </Link>
                      ) : (
                        <Text color="gray.400" fontSize="xs">N/A</Text>
                      )}
                    </HStack>
                  </Tooltip>
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
