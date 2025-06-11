import { useState } from 'react'
import {
  Box,
  Card,
  CardBody,
  HStack,
  VStack,
  Text,
  Badge,
  Button,
  Collapse,
  Divider,
  Grid,
  GridItem,
  Stat,
  StatLabel,
  StatNumber,
  StatHelpText,
  IconButton,
  Tooltip,
  Progress,
} from '@chakra-ui/react'
import { FaChevronDown, FaChevronUp, FaCopy } from 'react-icons/fa'
import { Process, ProcessStatus, ProcessStatusLabel, ProcessStatusColor } from '~types/api'

interface ProcessCardProps {
  process: Process
}

export const ProcessCard = ({ process }: ProcessCardProps) => {
  const [isExpanded, setIsExpanded] = useState(false)
  const [copied, setCopied] = useState(false)

  const handleCopyProcessId = () => {
    navigator.clipboard.writeText(process.id)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const formatDate = (dateString: string) => {
    try {
      const date = new Date(dateString)
      // Check for Go zero time (0001-01-01T00:00:00Z)
      if (date.getFullYear() <= 1) {
        return 'N/A'
      }
      // Check for invalid date
      if (isNaN(date.getTime())) {
        return 'Invalid date'
      }
      return date.toLocaleString()
    } catch {
      return 'Invalid date'
    }
  }

  const truncateId = (id: string) => {
    return `${id.slice(0, 8)}...${id.slice(-6)}`
  }

  return (
    <Card variant="outline" borderColor={isExpanded ? 'purple.200' : 'gray.200'}>
      <CardBody>
        <VStack align="stretch" spacing={4}>
          {/* Header Section */}
          <HStack justify="space-between" align="flex-start">
            <VStack align="flex-start" spacing={1}>
              <HStack>
                <Text fontWeight="bold" fontSize="sm" color="gray.600">
                  Process ID:
                </Text>
                <HStack spacing={1}>
                  <Text fontFamily="mono" fontSize="sm">
                    {truncateId(process.id)}
                  </Text>
                  <Tooltip label={copied ? 'Copied!' : 'Copy full ID'}>
                    <IconButton
                      aria-label="Copy process ID"
                      icon={<FaCopy />}
                      size="xs"
                      variant="ghost"
                      onClick={handleCopyProcessId}
                    />
                  </Tooltip>
                </HStack>
              </HStack>
              <HStack spacing={2}>
                <Badge colorScheme={ProcessStatusColor[process.status]}>
                  {ProcessStatusLabel[process.status]}
                </Badge>
                {process.isAcceptingVotes && (
                  <Badge colorScheme="green" variant="outline">
                    Accepting Votes
                  </Badge>
                )}
                {process.isFinalized && (
                  <Badge colorScheme="blue" variant="outline">
                    Finalized
                  </Badge>
                )}
              </HStack>
            </VStack>
            <Button
              size="sm"
              rightIcon={isExpanded ? <FaChevronUp /> : <FaChevronDown />}
              variant="ghost"
              onClick={() => setIsExpanded(!isExpanded)}
            >
              {isExpanded ? 'Hide' : 'Show'} Details
            </Button>
          </HStack>

          {/* Statistics Summary */}
          <Grid templateColumns="repeat(4, 1fr)" gap={4}>
            <GridItem>
              <Stat size="sm">
                <StatLabel>Verified Votes</StatLabel>
                <StatNumber>{process.sequencerStats.verifiedVotesCount}</StatNumber>
              </Stat>
            </GridItem>
            <GridItem>
              <Stat size="sm">
                <StatLabel>Pending Votes</StatLabel>
                <StatNumber>{process.sequencerStats.pendingVotesCount}</StatNumber>
              </Stat>
            </GridItem>
            <GridItem>
              <Stat size="sm">
                <StatLabel>State Transitions</StatLabel>
                <StatNumber>{process.sequencerStats.stateTransitionCount}</StatNumber>
              </Stat>
            </GridItem>
            <GridItem>
              <Stat size="sm">
                <StatLabel>Current Batch</StatLabel>
                <StatNumber>{process.sequencerStats.currentBatchSize}</StatNumber>
              </Stat>
            </GridItem>
          </Grid>

          {/* Expandable Details Section */}
          <Collapse in={isExpanded}>
            <VStack align="stretch" spacing={4} pt={4}>
              <Divider />

              {/* Process Information */}
              <Box>
                <Text fontWeight="bold" mb={2} color="gray.700">
                  Process Information
                </Text>
                <Grid templateColumns="repeat(2, 1fr)" gap={3}>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Organization ID:
                    </Text>
                    <Text fontSize="sm" fontFamily="mono">
                      {truncateId(process.organizationId)}
                    </Text>
                  </GridItem>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Start Time:
                    </Text>
                    <Text fontSize="sm">{formatDate(process.startTime)}</Text>
                  </GridItem>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Vote Count:
                    </Text>
                    <Text fontSize="sm">{process.voteCount}</Text>
                  </GridItem>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Vote Overwrites:
                    </Text>
                    <Text fontSize="sm">{process.voteOverwriteCount}</Text>
                  </GridItem>
                </Grid>
              </Box>

              <Divider />

              {/* Sequencer Statistics */}
              <Box>
                <Text fontWeight="bold" mb={2} color="gray.700">
                  Sequencer Statistics
                </Text>
                <Grid templateColumns="repeat(2, 1fr)" gap={3}>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Aggregated Votes:
                    </Text>
                    <Text fontSize="sm">{process.sequencerStats.aggregatedVotesCount}</Text>
                  </GridItem>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Settled Transitions:
                    </Text>
                    <Text fontSize="sm">{process.sequencerStats.settledStateTransitionCount}</Text>
                  </GridItem>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Last Batch Size:
                    </Text>
                    <Text fontSize="sm">{process.sequencerStats.lastBatchSize}</Text>
                  </GridItem>
                  <GridItem>
                    <Text fontSize="sm" color="gray.600">
                      Last Transition:
                    </Text>
                    <Text fontSize="sm">
                      {process.sequencerStats.lastStateTransitionDate
                        ? formatDate(process.sequencerStats.lastStateTransitionDate)
                        : 'N/A'}
                    </Text>
                  </GridItem>
                </Grid>
              </Box>

              {/* Census Information */}
              {process.census && (
                <>
                  <Divider />
                  <Box>
                    <Text fontWeight="bold" mb={2} color="gray.700">
                      Census Information
                    </Text>
                    <Grid templateColumns="repeat(2, 1fr)" gap={3}>
                      <GridItem>
                        <Text fontSize="sm" color="gray.600">
                          Census Root:
                        </Text>
                        <Text fontSize="sm" fontFamily="mono">
                          {truncateId(process.census.censusRoot)}
                        </Text>
                      </GridItem>
                      <GridItem>
                        <Text fontSize="sm" color="gray.600">
                          Max Votes:
                        </Text>
                        <Text fontSize="sm">{process.census.maxVotes}</Text>
                      </GridItem>
                    </Grid>
                  </Box>
                </>
              )}

              {/* Results Section - Show when status is RESULTS */}
              {process.status === ProcessStatus.RESULTS && process.result && process.result.length > 0 && (
                <>
                  <Divider />
                  <Box>
                    <Text fontWeight="bold" mb={3} color="gray.700">
                      Results
                    </Text>
                    <VStack align="stretch" spacing={3}>
                      {(() => {
                        // Calculate total votes
                        const totalVotes = process.result.reduce((sum, votes) => {
                          return sum + BigInt(votes)
                        }, BigInt(0))
                        
                        return process.result
                          .map((result, index) => ({ result, index }))
                          .filter(({ result }) => BigInt(result) > 0)
                          .map(({ result, index }) => {
                            const votes = BigInt(result)
                            const percentage = totalVotes > 0 
                              ? Number((votes * BigInt(100)) / totalVotes) 
                              : 0
                            
                            return (
                              <Box key={index}>
                                <HStack justify="space-between" mb={1}>
                                  <Text fontSize="sm" fontWeight="medium" color="gray.700">
                                    Option {index + 1}
                                  </Text>
                                  <HStack spacing={2}>
                                    <Text fontSize="sm" fontWeight="semibold" color="purple.500">
                                      {percentage.toFixed(1)}%
                                    </Text>
                                    <Text fontSize="xs" color="gray.500">
                                      ({result} votes)
                                    </Text>
                                  </HStack>
                                </HStack>
                                <Progress 
                                  value={percentage} 
                                  size="sm" 
                                  colorScheme="purple"
                                  borderRadius="full"
                                  hasStripe
                                  isAnimated
                                />
                              </Box>
                            )
                          })
                      })()}
                    </VStack>
                  </Box>
                </>
              )}
            </VStack>
          </Collapse>
        </VStack>
      </CardBody>
    </Card>
  )
}
