import {
  VStack,
  Table,
  Thead,
  Tbody,
  Tr,
  Th,
  Td,
  Text,
  Card,
  CardBody,
  Alert,
  AlertIcon,
  Skeleton,
  Badge,
  HStack,
  Checkbox,
} from '@chakra-ui/react'
import { useState, useMemo } from 'react'
import { useWorkers } from '~hooks/useSequencerAPI'
import { Worker } from '~types/api'

export const Workers = () => {
  const [hideZeroJobs, setHideZeroJobs] = useState(true)
  const { data, isLoading, error } = useWorkers({
    refetchInterval: 10000, // Refresh every 10 seconds
  })

  const filteredAndSortedWorkers = useMemo(() => {
    if (!data?.workers) return []
    
    let workers = [...data.workers]
    
    // Filter out workers with zero jobs if checkbox is checked
    if (hideZeroJobs) {
      workers = workers.filter(w => w.successCount > 0 || w.failedCount > 0)
    }
    
    // Sort by success count (descending)
    workers.sort((a, b) => b.successCount - a.successCount)
    
    return workers
  }, [data?.workers, hideZeroJobs])

  if (error) {
    return (
      <Alert status="error" borderRadius="md">
        <AlertIcon />
        Failed to load workers: {error.message}
      </Alert>
    )
  }

  return (
    <Card>
      <CardBody>
        <VStack align="stretch" spacing={4}>
          <HStack justify="space-between">
            <Text fontSize="lg" fontWeight="semibold" color="gray.700">
              Worker Nodes ({filteredAndSortedWorkers.length})
            </Text>
            <Checkbox
              isChecked={hideZeroJobs}
              onChange={(e) => setHideZeroJobs(e.target.checked)}
              colorScheme="purple"
            >
              Hide workers with no jobs
            </Checkbox>
          </HStack>

          {isLoading ? (
            <VStack spacing={2}>
              {[...Array(3)].map((_, i) => (
                <Skeleton key={i} height="60px" width="100%" borderRadius="md" />
              ))}
            </VStack>
          ) : filteredAndSortedWorkers.length === 0 ? (
            <Alert status="info" borderRadius="md">
              <AlertIcon />
              {hideZeroJobs 
                ? 'No workers with completed jobs found. Uncheck the filter to see all workers.'
                : 'No workers found.'
              }
            </Alert>
          ) : (
            <Table variant="simple" size="sm">
              <Thead>
                <Tr>
                  <Th>Worker Name</Th>
                  <Th isNumeric>Successful Jobs</Th>
                  <Th isNumeric>Failed Jobs</Th>
                  <Th isNumeric>Success Rate</Th>
                </Tr>
              </Thead>
              <Tbody>
                {filteredAndSortedWorkers.map((worker: Worker) => {
                  const totalJobs = worker.successCount + worker.failedCount
                  const successRate = totalJobs > 0 
                    ? ((worker.successCount / totalJobs) * 100).toFixed(1)
                    : '0'
                  
                  return (
                    <Tr key={worker.name}>
                      <Td>
                        <Badge colorScheme="purple" fontFamily="mono" fontSize="xs">
                          {worker.name}
                        </Badge>
                      </Td>
                      <Td isNumeric>
                        <Text color="green.600" fontWeight="medium">
                          {worker.successCount.toLocaleString()}
                        </Text>
                      </Td>
                      <Td isNumeric>
                        <Text color={worker.failedCount > 0 ? 'red.600' : 'gray.600'}>
                          {worker.failedCount.toLocaleString()}
                        </Text>
                      </Td>
                      <Td isNumeric>
                        <Badge 
                          colorScheme={
                            parseFloat(successRate) >= 95 ? 'green' :
                            parseFloat(successRate) >= 80 ? 'yellow' :
                            'red'
                          }
                        >
                          {successRate}%
                        </Badge>
                      </Td>
                    </Tr>
                  )
                })}
              </Tbody>
            </Table>
          )}
        </VStack>
      </CardBody>
    </Card>
  )
}
