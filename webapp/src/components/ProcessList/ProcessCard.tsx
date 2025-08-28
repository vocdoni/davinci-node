import { useState, useMemo } from 'react'
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
  IconButton,
  Tooltip,
  Progress,
  Flex,
  Icon,
  Wrap,
  WrapItem,
  useColorModeValue,
  Link,
} from '@chakra-ui/react'
import {
  FaChevronDown,
  FaChevronUp,
  FaCopy,
  FaClock,
  FaCalendarAlt,
  FaUsers,
  FaVoteYea,
  FaLink,
  FaKey,
  FaFingerprint,
  FaCheckCircle,
  FaExternalLinkAlt,
} from 'react-icons/fa'
import { Process, ProcessStatus, ProcessStatusLabel, ProcessStatusColor } from '~types/api'

interface ProcessCardProps {
  process: Process
}

export const ProcessCard = ({ process }: ProcessCardProps) => {
  const [isExpanded, setIsExpanded] = useState(false)
  const [copiedField, setCopiedField] = useState<string | null>(null)
  const bgColor = useColorModeValue('white', 'gray.800')
  const borderColor = useColorModeValue(
    isExpanded ? 'purple.200' : 'gray.200',
    isExpanded ? 'purple.600' : 'gray.600'
  )

  const handleCopy = async (text: string, fieldName: string) => {
    try {
      // Use the Clipboard API if available (requires HTTPS)
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text)
      } else {
        // Fallback for HTTP contexts
        const textArea = document.createElement('textarea')
        textArea.value = text
        textArea.style.position = 'fixed'
        textArea.style.left = '-999999px'
        textArea.style.top = '-999999px'
        document.body.appendChild(textArea)
        textArea.focus()
        textArea.select()
        document.execCommand('copy')
        textArea.remove()
      }
      setCopiedField(fieldName)
      setTimeout(() => setCopiedField(null), 2000)
    } catch (error) {
      console.error('Failed to copy:', error)
    }
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

  const formatDuration = (durationNs: string | number) => {
    try {
      const ns = typeof durationNs === 'string' ? parseInt(durationNs) : durationNs
      if (isNaN(ns) || ns <= 0) return 'N/A'

      // Go sends duration in nanoseconds, convert to milliseconds
      const ms = Math.floor(ns / 1e6)

      // Sanity check - if still too large, it's probably invalid
      if (ms > 100 * 365 * 24 * 60 * 60 * 1000) { // 100 years
        console.warn('Duration too large:', ns)
        return 'N/A'
      }

      const seconds = Math.floor(ms / 1000)
      const minutes = Math.floor(seconds / 60)
      const hours = Math.floor(minutes / 60)
      const days = Math.floor(hours / 24)

      if (days > 0) return `${days}d ${hours % 24}h`
      if (hours > 0) return `${hours}h ${minutes % 60}m`
      if (minutes > 0) return `${minutes}m ${seconds % 60}s`
      return `${seconds}s`
    } catch (error) {
      console.error('Error formatting duration:', error)
      return 'N/A'
    }
  }

  // Calculate end time and remaining time
  const processTimings = useMemo(() => {
    try {
      // Debug logging
      console.log('Process timing calculation:', {
        processId: process.id,
        startTime: process.startTime,
        duration: process.duration,
      })

      const startTime = new Date(process.startTime)
      // Check if start time is valid
      if (isNaN(startTime.getTime()) || startTime.getFullYear() < 1970) {
        console.warn('Invalid start time:', process.startTime)
        return {
          endTime: null,
          remainingMs: 0,
          isActive: false,
          remainingFormatted: 'N/A',
        }
      }

      // Parse duration - Go sends it in nanoseconds
      const durationNs = parseInt(process.duration)
      if (isNaN(durationNs) || durationNs <= 0) {
        console.warn('Invalid duration:', process.duration)
        return {
          endTime: null,
          remainingMs: 0,
          isActive: false,
          remainingFormatted: 'N/A',
        }
      }

      // Convert nanoseconds to milliseconds
      const durationMs = Math.floor(durationNs / 1e6)

      // Cap duration to a reasonable maximum (100 years in milliseconds)
      const maxDuration = 100 * 365 * 24 * 60 * 60 * 1000
      if (durationMs > maxDuration) {
        console.warn('Duration exceeds maximum:', durationNs, 'ns =', durationMs, 'ms')
        return {
          endTime: null,
          remainingMs: 0,
          isActive: false,
          remainingFormatted: 'N/A',
        }
      }

      const endTimeMs = startTime.getTime() + durationMs
      // Check for overflow
      if (!isFinite(endTimeMs) || endTimeMs > 8640000000000000) { // Max date in JS
        console.warn('End time calculation overflow')
        return {
          endTime: null,
          remainingMs: 0,
          isActive: false,
          remainingFormatted: 'N/A',
        }
      }

      const endTime = new Date(endTimeMs)
      // Validate end time
      if (isNaN(endTime.getTime())) {
        console.warn('Invalid end time after calculation')
        return {
          endTime: null,
          remainingMs: 0,
          isActive: false,
          remainingFormatted: 'N/A',
        }
      }

      const now = new Date()
      const remainingMs = endTime.getTime() - now.getTime()
      const isActive = remainingMs > 0 && process.status === ProcessStatus.READY

      console.log('Remaining time calculation:', {
        endTime: endTime.toISOString(),
        now: now.toISOString(),
        remainingMs,
        isActive,
        status: process.status,
      })

      return {
        endTime,
        remainingMs,
        isActive,
        remainingFormatted: remainingMs > 0 ? formatDuration(remainingMs * 1e6) : 'Ended', // Convert back to nanoseconds for formatDuration
      }
    } catch (error) {
      console.error('Error calculating process timings:', error, {
        processId: process.id,
        startTime: process.startTime,
        duration: process.duration,
      })
      return {
        endTime: null,
        remainingMs: 0,
        isActive: false,
        remainingFormatted: 'N/A',
      }
    }
  }, [process.startTime, process.duration, process.status, process.id])

  const HexField = ({ label, value, fieldName }: { label: string; value: string; fieldName: string }) => {
    // Show first 6 and last 4 characters for hex strings
    const displayValue = value.length > 12
      ? `${value.slice(0, 6)}...${value.slice(-4)}`
      : value

    return (
      <HStack spacing={1} align="center">
        <Text fontSize="sm" color="gray.600">{label}:</Text>
        <Tooltip label={value} placement="top">
          <Text fontSize="sm" fontFamily="mono" cursor="pointer">{displayValue}</Text>
        </Tooltip>
        <Tooltip label={copiedField === fieldName ? 'Copied!' : 'Copy'}>
          <IconButton
            aria-label={`Copy ${label}`}
            icon={<FaCopy />}
            size="xs"
            variant="ghost"
            onClick={() => handleCopy(value, fieldName)}
          />
        </Tooltip>
      </HStack>
    )
  }

  const CompactField = ({ label, value, icon }: { label: string; value: string | number; icon?: React.ComponentType }) => (
    <HStack spacing={2} align="center">
      {icon && <Icon as={icon} color="gray.500" boxSize={3} />}
      <Text fontSize="sm" color="gray.600">{label}:</Text>
      <Text fontSize="sm" fontWeight="medium">{value}</Text>
    </HStack>
  )

  const BallotModeVisualization = () => {
    const mode = process.ballotMode
    return (
      <Box p={3} bg={useColorModeValue('purple.50', 'purple.900')} borderRadius="md">
        <Text fontWeight="bold" mb={3} color="purple.700" fontSize="sm">
          Voting Configuration
        </Text>
        <Grid templateColumns="repeat(2, 1fr)" gap={3}>
          <GridItem>
            <VStack align="start" spacing={2}>
              <HStack>
                <Icon as={FaVoteYea} color="purple.500" boxSize={4} />
                <Text fontSize="xs" fontWeight="medium">Choices</Text>
              </HStack>
              <Box pl={6}>
                <Text fontSize="xs">Max options: {mode.numFields}</Text>
                <Text fontSize="xs">Value range: {mode.minValue} - {mode.maxValue}</Text>
                {mode.uniqueValues && (
                  <Badge colorScheme="purple" size="xs" mt={1}>Unique choices required</Badge>
                )}
              </Box>
            </VStack>
          </GridItem>
          <GridItem>
            <VStack align="start" spacing={2}>
              <HStack>
                <Icon as={FaUsers} color="purple.500" boxSize={4} />
                <Text fontSize="xs" fontWeight="medium">Vote Weight</Text>
              </HStack>
              <Box pl={6}>
                {mode.costFromWeight ? (
                  <>
                    <Text fontSize="xs">Based on census weight</Text>
                    {mode.costExponent > 1 && (
                      <Text fontSize="xs">Exponent: {mode.costExponent}</Text>
                    )}
                  </>
                ) : (
                  <>
                    <Text fontSize="xs">Total budget: {mode.maxValueSum}</Text>
                    {mode.minTotalCost !== '0' && (
                      <Text fontSize="xs">Min required: {mode.minTotalCost}</Text>
                    )}
                  </>
                )}
              </Box>
            </VStack>
          </GridItem>
        </Grid>
      </Box>
    )
  }

  return (
    <Card variant="outline" borderColor={borderColor} bg={bgColor} id={process.id}>
      <CardBody>
        <VStack align="stretch" spacing={4}>
          {/* Header Section */}
          <Flex justify="space-between" align="flex-start" wrap="wrap" gap={2}>
            <VStack align="flex-start" spacing={2} flex={1}>
              <HexField label="Process ID" value={process.id} fieldName="processId" />
              <Wrap spacing={2}>
                <WrapItem>
                  <Badge colorScheme={ProcessStatusColor[process.status]} size="sm">
                    {ProcessStatusLabel[process.status]}
                  </Badge>
                </WrapItem>
                {process.isAcceptingVotes && (
                  <WrapItem>
                    <Badge colorScheme="green" variant="outline" size="sm">
                      <Icon as={FaCheckCircle} mr={1} />
                      Accepting Votes
                    </Badge>
                  </WrapItem>
                )}
                {process.status === ProcessStatus.RESULTS && (
                  <WrapItem>
                    <Badge colorScheme="blue" variant="outline" size="sm">
                      <Icon as={FaCheckCircle} mr={1} />
                      Finalized
                    </Badge>
                  </WrapItem>
                )}
                {processTimings.isActive && (
                  <WrapItem>
                    <Badge colorScheme="orange" variant="solid" size="sm">
                      <Icon as={FaClock} mr={1} />
                      {processTimings.remainingFormatted} left
                    </Badge>
                  </WrapItem>
                )}
              </Wrap>
            </VStack>
            <Button
              size="sm"
              rightIcon={isExpanded ? <FaChevronUp /> : <FaChevronDown />}
              variant="ghost"
              onClick={() => setIsExpanded(!isExpanded)}
            >
              {isExpanded ? 'Hide' : 'Show'} Details
            </Button>
          </Flex>

          {/* Quick Stats Grid */}
          <Grid templateColumns="repeat(auto-fit, minmax(120px, 1fr))" gap={3}>
            <GridItem>
              <Stat size="sm">
                <StatLabel fontSize="xs">Verified</StatLabel>
                <StatNumber fontSize="md">{process.sequencerStats.verifiedVotesCount}</StatNumber>
              </Stat>
            </GridItem>
            <GridItem>
              <Stat size="sm">
                <StatLabel fontSize="xs">Pending</StatLabel>
                <StatNumber fontSize="md">{process.sequencerStats.pendingVotesCount}</StatNumber>
              </Stat>
            </GridItem>
            <GridItem>
              <Stat size="sm">
                <StatLabel fontSize="xs">Transitions</StatLabel>
                <StatNumber fontSize="md">{process.sequencerStats.stateTransitionCount}</StatNumber>
              </Stat>
            </GridItem>
            <GridItem>
              <Stat size="sm">
                <StatLabel fontSize="xs">Current Batch</StatLabel>
                <StatNumber fontSize="md">{process.sequencerStats.currentBatchSize}</StatNumber>
              </Stat>
            </GridItem>
          </Grid>

          {/* Time Information Bar */}
          <HStack spacing={4} p={2} bg={useColorModeValue('gray.50', 'gray.700')} borderRadius="md" fontSize="sm">
            <CompactField label="Duration" value={formatDuration(process.duration)} icon={FaClock} />
            <Divider orientation="vertical" h="20px" />
            <CompactField
              label="End"
              value={(() => {
                if (!processTimings.endTime) return 'N/A'
                try {
                  // Check if date is valid before formatting
                  if (isNaN(processTimings.endTime.getTime())) return 'N/A'
                  return processTimings.endTime.toLocaleString()
                } catch {
                  return 'N/A'
                }
              })()}
              icon={FaCalendarAlt}
            />
          </HStack>

          {/* Expandable Details Section */}
          <Collapse in={isExpanded}>
            <VStack align="stretch" spacing={4} pt={2}>
              <Divider />

              {/* Ballot Mode Visualization */}
              <BallotModeVisualization />

              {/* Process Details Grid */}
              <Box>
                <Text fontWeight="bold" mb={2} color="gray.700" fontSize="sm">
                  Process Details
                </Text>
                <Grid templateColumns="repeat(2, 1fr)" gap={3}>
                  <GridItem>
                    <VStack align="start" spacing={2}>
                      <HexField label="Organization" value={process.organizationId} fieldName="orgId" />
                      <CompactField label="Total Votes" value={process.voteCount} />
                      <CompactField label="Vote Changes" value={process.voteOverwrittenCount} />
                      <CompactField label="Start Time" value={formatDate(process.startTime)} />
                    </VStack>
                  </GridItem>
                  <GridItem>
                    <VStack align="start" spacing={2}>
                      <HexField label="State Root" value={process.stateRoot} fieldName="stateRoot" />
                      {process.metadataURI && (
                        <HStack spacing={1}>
                          <Icon as={FaLink} color="gray.500" boxSize={3} />
                          <Text fontSize="sm" color="gray.600">Metadata:</Text>
                          <Link href={process.metadataURI} isExternal color="purple.500" fontSize="sm">
                            View <Icon as={FaExternalLinkAlt} ml={1} />
                          </Link>
                        </HStack>
                      )}
                    </VStack>
                  </GridItem>
                </Grid>
              </Box>

              <Divider />

              {/* Encryption Key */}
              {process.encryptionKey && (
                <>
                  <Box>
                    <Text fontWeight="bold" mb={2} color="gray.700" fontSize="sm">
                      <Icon as={FaKey} mr={2} />
                      Encryption Key
                    </Text>
                    <VStack align="start" spacing={2}>
                      <HexField label="X" value={process.encryptionKey.x} fieldName="encKeyX" />
                      <HexField label="Y" value={process.encryptionKey.y} fieldName="encKeyY" />
                    </VStack>
                  </Box>
                  <Divider />
                </>
              )}

              {/* Census Information */}
              {process.census && (
                <>
                  <Box>
                    <Text fontWeight="bold" mb={2} color="gray.700" fontSize="sm">
                      <Icon as={FaUsers} mr={2} />
                      Census Information
                    </Text>
                    <Grid templateColumns="repeat(2, 1fr)" gap={3}>
                      <GridItem>
                        <VStack align="start" spacing={2}>
                          <HexField label="Root" value={process.census.censusRoot} fieldName="censusRoot" />
                          <CompactField label="Max Votes" value={process.census.maxVotes} />
                          <CompactField label="Origin" value={process.census.censusOrigin} />
                        </VStack>
                      </GridItem>
                      <GridItem>
                        {process.census.censusURI && (
                          <HStack spacing={1}>
                            <Icon as={FaLink} color="gray.500" boxSize={3} />
                            <Text fontSize="sm" color="gray.600">Census URI:</Text>
                            <Link href={process.census.censusURI} isExternal color="purple.500" fontSize="sm">
                              View <Icon as={FaExternalLinkAlt} ml={1} />
                            </Link>
                          </HStack>
                        )}
                      </GridItem>
                    </Grid>
                  </Box>
                  <Divider />
                </>
              )}

              {/* Sequencer Statistics Details */}
              <Box>
                <Text fontWeight="bold" mb={2} color="gray.700" fontSize="sm">
                  <Icon as={FaFingerprint} mr={2} />
                  Sequencer Details
                </Text>
                <Grid templateColumns="repeat(2, 1fr)" gap={3}>
                  <GridItem>
                    <VStack align="start" spacing={2}>
                      <CompactField label="Aggregated" value={process.sequencerStats.aggregatedVotesCount} />
                      <CompactField label="Settled" value={process.sequencerStats.settledStateTransitionCount} />
                      <CompactField label="Last Batch" value={process.sequencerStats.lastBatchSize} />
                    </VStack>
                  </GridItem>
                  <GridItem>
                    <VStack align="start" spacing={2}>
                      <CompactField
                        label="Last Transition"
                        value={formatDate(process.sequencerStats.lastStateTransitionDate)}
                      />
                    </VStack>
                  </GridItem>
                </Grid>
              </Box>

              {/* Results Section */}
              {process.status === ProcessStatus.RESULTS && process.result && process.result.length > 0 && (
                <>
                  <Divider />
                  <Box>
                    <Text fontWeight="bold" mb={3} color="gray.700" fontSize="sm">
                      Results
                    </Text>
                    <VStack align="stretch" spacing={3}>
                      {(() => {
                        // Use numFields from ballotMode to determine how many options to show
                        const numFields = process.ballotMode.numFields
                        const resultsToShow = process.result.slice(0, numFields)

                        const totalVotes = resultsToShow.reduce((sum, votes) => {
                          return sum + BigInt(votes)
                        }, BigInt(0))

                        return resultsToShow
                          .map((result, index) => {
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
