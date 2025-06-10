// API Response Types

export interface ContractAddresses {
  process: string
  organization: string
  stateTransitionVerifier: string
  resultsVerifier: string
}

export interface InfoResponse {
  circuitUrl: string
  circuitHash: string
  provingKeyUrl: string
  provingKeyHash: string
  verificationKeyUrl: string
  verificationKeyHash: string
  ballotProofWasmHelperUrl: string
  ballotProofWasmHelperHash: string
  ballotProofWasmHelperExecJsUrl: string
  ballotProofWasmHelperExecJsHash: string
  contracts: ContractAddresses
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
  address: string
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
  maxCount: number
  maxValue: string
  minValue: string
  forceUniqueness: boolean
  costFromWeight: boolean
  costExponent: number
  maxTotalCost: string
  minTotalCost: string
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
  voteCount: string
  voteOverwriteCount: string
  isFinalized: boolean
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

export const ProcessStatusColor: Record<number, string> = {
  [ProcessStatus.READY]: 'green',
  [ProcessStatus.ENDED]: 'gray',
  [ProcessStatus.CANCELED]: 'red',
  [ProcessStatus.PAUSED]: 'orange',
  [ProcessStatus.RESULTS]: 'blue',
}
