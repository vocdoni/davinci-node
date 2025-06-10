import {
  Box,
  Heading,
  VStack,
  HStack,
  Text,
  Link,
  Skeleton,
  Badge,
  Card,
  CardBody,
  Divider,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
} from '@chakra-ui/react'
import { FaExternalLinkAlt } from 'react-icons/fa'
import { ContractAddresses, SequencerStatsResponse } from '~types/api'
import { useSequencerStats } from '~hooks/useSequencerAPI'

interface ContractLinksProps {
  contracts?: ContractAddresses
  isLoading: boolean
}

export const ContractLinks = ({ contracts, isLoading }: ContractLinksProps) => {
  const blockExplorerUrl = import.meta.env.BLOCK_EXPLORER_URL || 'https://sepolia.etherscan.io/address'
  const { data: stats, isLoading: statsLoading } = useSequencerStats({
    refetchInterval: 10000, // Refresh every 10 seconds
  })

  const contractItems = [
    { label: 'Process Registry', address: contracts?.process },
    { label: 'Organization Registry', address: contracts?.organization },
  ]

  const statItems = [
    { emoji: '🗳️', label: 'Active Processes', value: stats?.activeProcesses },
    { emoji: '⏳', label: 'Pending Votes', value: stats?.pendingVotes },
    { emoji: '✅', label: 'Verified Votes', value: stats?.verifiedVotes },
    { emoji: '📦', label: 'Aggregated Votes', value: stats?.aggregatedVotes },
    { emoji: '⚡', label: 'State Transitions', value: stats?.stateTransitions },
    { emoji: '💎', label: 'Settled Transitions', value: stats?.settledStateTransitions },
  ]

  return (
    <Card>
      <CardBody>
        <VStack align="stretch" spacing={4}>
          {/* Sequencer Statistics */}
          <VStack align="stretch" spacing={4}>
            <Heading size="md" color="gray.700">
              Sequencer Statistics
            </Heading>
            <SimpleGrid columns={{ base: 2, md: 3 }} spacing={4}>
              {statItems.map((item) => (
                <Box key={item.label}>
                  <Stat>
                    <StatLabel fontSize="sm" color="gray.600">
                      <HStack spacing={1}>
                        <Text>{item.emoji}</Text>
                        <Text>{item.label}</Text>
                      </HStack>
                    </StatLabel>
                    {statsLoading ? (
                      <Skeleton height="24px" width="60px" mt={1} />
                    ) : (
                      <StatNumber fontSize="lg" color="purple.600">
                        {item.value?.toLocaleString() || '0'}
                      </StatNumber>
                    )}
                  </Stat>
                </Box>
              ))}
            </SimpleGrid>
          </VStack>

          <Divider />

          {/* Smart Contracts */}
          <VStack align="stretch" spacing={4}>
            <Heading size="md" color="gray.700">
              Smart Contracts
            </Heading>
            <VStack align="stretch" spacing={3}>
              {contractItems.map((item) => (
                <HStack key={item.label} justify="space-between">
                  <Text fontWeight="medium" color="gray.600">
                    {item.label}:
                  </Text>
                  {isLoading ? (
                    <Skeleton height="20px" width="200px" />
                  ) : item.address ? (
                    <HStack>
                      <Badge colorScheme="purple" fontFamily="mono" fontSize="xs">
                        {item.address.slice(0, 6)}...{item.address.slice(-4)}
                      </Badge>
                      <Link
                        href={`${blockExplorerUrl}/${item.address}`}
                        isExternal
                        color="purple.500"
                        _hover={{ color: 'purple.600' }}
                      >
                        <FaExternalLinkAlt size={12} />
                      </Link>
                    </HStack>
                  ) : (
                    <Text color="gray.400">Not available</Text>
                  )}
                </HStack>
              ))}
            </VStack>
          </VStack>
        </VStack>
      </CardBody>
    </Card>
  )
}
