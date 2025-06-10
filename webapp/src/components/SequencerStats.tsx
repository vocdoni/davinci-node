import {
  Box,
  Heading,
  VStack,
  SimpleGrid,
  Stat,
  StatLabel,
  StatNumber,
  Text,
  Card,
  CardBody,
  Skeleton,
  HStack,
} from '@chakra-ui/react'
import { useSequencerStats } from '~hooks/useSequencerAPI'

export const SequencerStats = () => {
  const { data: stats, isLoading: statsLoading } = useSequencerStats({
    refetchInterval: 10000, // Refresh every 10 seconds
  })

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
          <Heading size="md" color="gray.700">
            Sequencer Statistics
          </Heading>
          <SimpleGrid columns={{ base: 2, md: 3, lg: 6 }} spacing={4}>
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
      </CardBody>
    </Card>
  )
}
