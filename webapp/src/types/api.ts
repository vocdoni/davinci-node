// API Response Types

export interface NetworkInfo {
  chainID: number
  shortName: string
  processRegistryContract: string
  processIDVersion: string
}

export interface InfoResponse {
  circuitUrl: string
  circuitHash: string
  provingKeyUrl: string
  provingKeyHash: string
  verificationKeyUrl: string
  verificationKeyHash: string
  networks: Record<string, NetworkInfo>
}

export interface SequencerStatsResponse {
  activeProcesses: number
  pendingVotes: number
  verifiedVotes: number
  aggregatedVotes: number
  stateTransitions: number
  settledStateTransitions: number
}

export interface Worker {
  name: string
  successCount: number
  failedCount: number
}

export interface WorkersResponse {
  workers: Worker[]
}

export interface ProcessListResponse {
  processes: string[]
}

export interface SequencerProcessStats {
  stateTransitionCount: number
  lastStateTransitionDate: string
  settledStateTransitionCount: number
  aggregatedVotesCount: number
  verifiedVotesCount: number
  pendingVotesCount: number
  currentBatchSize: number
  lastBatchSize: number
}

export interface BallotMode {
  numFields: number
  maxValue: string
  minValue: string
  uniqueValues: boolean
  costExponent: number
  maxValueSum: string
  minValueSum: string
}

export interface Census {
  censusOrigin: number
  maxVotes: string
  censusRoot: string
  censusURI: string
}

export interface EncryptionKey {
  x: string
  y: string
}

export interface Process {
  id: string
  status: number
  organizationId: string
  encryptionKey: EncryptionKey
  stateRoot: string
  result: string[]
  startTime: string
  duration: string
  metadataURI: string
  ballotMode: BallotMode
  census: Census
  votersCount: string
  overwrittenVotesCount: string
  isAcceptingVotes: boolean
  sequencerStats: SequencerProcessStats
}

// Process Status Constants
export const ProcessStatus = {
  READY: 0,
  ENDED: 1,
  CANCELED: 2,
  PAUSED: 3,
  RESULTS: 4,
} as const

export type ProcessStatusType = typeof ProcessStatus[keyof typeof ProcessStatus]

export const ProcessStatusLabel: Record<number, string> = {
  [ProcessStatus.READY]: 'Ready',
  [ProcessStatus.ENDED]: 'Ended',
  [ProcessStatus.CANCELED]: 'Canceled',
  [ProcessStatus.PAUSED]: 'Paused',
  [ProcessStatus.RESULTS]: 'Results',
}

/**
 * Extracts the version bytes from a process ID hex string and returns them
 * as a lowercase hex string (e.g. "0x12345678").
 * The version occupies bytes 20-23 (4 bytes / 8 hex chars) of the 31-byte process ID.
 */
export function processIDVersion(processIDHex: string): string {
  const hex = processIDHex.startsWith("0x")
    ? processIDHex.slice(2)
    : processIDHex

  // ProcessIDLen = 31 bytes = 62 hex chars
  if (hex.length !== 62) {
    throw new Error(`invalid process ID hex length ${hex.length}, want 62`)
  }

  if (!/^[0-9a-fA-F]+$/.test(hex)) {
    throw new Error("invalid process ID hex string")
  }

  // address = 20 bytes = 40 hex chars
  // version = next 4 bytes = 8 hex chars
  return "0x" + hex.slice(40, 48).toLowerCase()
}

export const ProcessStatusColor: Record<number, string> = {
  [ProcessStatus.READY]: 'green',
  [ProcessStatus.ENDED]: 'gray',
  [ProcessStatus.CANCELED]: 'red',
  [ProcessStatus.PAUSED]: 'orange',
  [ProcessStatus.RESULTS]: 'blue',
}
