import { useState, useMemo } from 'react'
import {
  Box,
  VStack,
  Heading,
  Alert,
  AlertIcon,
  AlertTitle,
  AlertDescription,
  Skeleton,
  Text,
  Select,
  HStack,
  Checkbox,
} from '@chakra-ui/react'
import { useProcessList, useProcesses } from '~hooks/useSequencerAPI'
import { Process, ProcessStatus, ProcessStatusLabel } from '~types/api'
import { ProcessCard } from './ProcessCard'

export const ProcessList = () => {
  const [statusFilter, setStatusFilter] = useState<'all' | number>('all')
  const [showOnlyWithVerifiedVotes, setShowOnlyWithVerifiedVotes] = useState(true)
  const { data: processList, error, isLoading } = useProcessList()
  const { data: processes = [], isLoading: processesLoading } = useProcesses(processList?.processes || [])

  // Sort and filter processes
  const sortedProcesses = useMemo(() => {
    if (!processes.length) return []

    let filtered = [...processes]

    // Apply status filter
    if (statusFilter !== 'all') {
      filtered = filtered.filter((p) => p.status === statusFilter)
    }

    // Apply verified votes filter
    if (showOnlyWithVerifiedVotes) {
      filtered = filtered.filter((p) => p.sequencerStats.verifiedVotesCount > 0)
    }

    // Sort: 
    // 1. First show processes that are not finalized and status=READY
    // 2. Then show the rest ordered by date (most recent first)
    return filtered.sort((a, b) => {
      // Group 1: Not finalized and status=READY
      const aIsReadyNotFinalized = !a.isFinalized && a.status === ProcessStatus.READY
      const bIsReadyNotFinalized = !b.isFinalized && b.status === ProcessStatus.READY
      
      // If different groups, prioritize READY not finalized
      if (aIsReadyNotFinalized && !bIsReadyNotFinalized) return -1
      if (!aIsReadyNotFinalized && bIsReadyNotFinalized) return 1
      
      // Within the same group or for all others, sort by date (most recent first)
      const aDate = new Date(a.startTime).getTime()
      const bDate = new Date(b.startTime).getTime()
      return bDate - aDate
    })
  }, [processes, statusFilter, showOnlyWithVerifiedVotes])

  if (error) {
    return (
      <Alert status="error">
        <AlertIcon />
        <AlertTitle>Error loading processes</AlertTitle>
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    )
  }

  return (
    <VStack align="stretch" spacing={6}>
      <HStack justify="space-between" align="flex-end">
        <Heading size="md" color="gray.700">
          Active Processes
        </Heading>
        <HStack spacing={4}>
          <Checkbox
            isChecked={showOnlyWithVerifiedVotes}
            onChange={(e) => setShowOnlyWithVerifiedVotes(e.target.checked)}
            size="sm"
          >
            <Text fontSize="sm">Only show processes with verified votes</Text>
          </Checkbox>
          <Select
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value === 'all' ? 'all' : parseInt(e.target.value))}
            size="sm"
            width="200px"
          >
            <option value="all">All Statuses</option>
            {Object.entries(ProcessStatus).map(([key, value]) => (
              <option key={key} value={value}>
                {ProcessStatusLabel[value]}
              </option>
            ))}
          </Select>
        </HStack>
      </HStack>

      {isLoading || processesLoading ? (
        <VStack spacing={4}>
          {[1, 2, 3].map((i) => (
            <Skeleton key={i} height="200px" width="100%" />
          ))}
        </VStack>
      ) : sortedProcesses.length === 0 ? (
        <Box p={8} textAlign="center" bg="gray.50" borderRadius="md">
          <Text color="gray.500">No processes found</Text>
        </Box>
      ) : (
        <VStack spacing={4} align="stretch">
          {sortedProcesses.map((process) => (
            <ProcessCard key={process.id} process={process} />
          ))}
        </VStack>
      )}
    </VStack>
  )
}
