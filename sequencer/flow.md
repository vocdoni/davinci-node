# Vote Processing Flow

This document describes the complete lifecycle of a vote through the DaVinci sequencer system, including all possible paths, status transitions, and error handling.

## Overview

Votes flow through five main stages:
1. **Ballot Submission** - API receives and validates ballot
2. **Vote Verification** - ZK proof generation for individual votes
3. **Proof Aggregation** - Batching and aggregating multiple vote proofs
4. **State Transition** - Updating process state with aggregated votes
5. **On-Chain Settlement** - Submitting state transitions to blockchain

## Vote ID Status Lifecycle

```
┌─────────┐
│ PENDING │ ← Initial state when ballot is submitted
└────┬────┘
     │
     ▼
┌──────────┐
│ VERIFIED │ ← Vote proof successfully generated
└────┬─────┘
     │
     ▼
┌────────────┐
│ AGGREGATED │ ← Vote included in aggregation batch
└─────┬──────┘
     │
     ▼
┌───────────┐
│ PROCESSED │ ← State transition proof generated
└─────┬─────┘
     │
     ▼
┌─────────┐
│ SETTLED │ ← Final state - on-chain confirmation (IMMUTABLE)
└─────────┘

Error/Terminal States:
┌───────┐  ← Any stage can transition to ERROR
│ ERROR │
└───────┘

┌─────────┐  ← Process timeout before settlement
│ TIMEOUT │
└─────────┘
```

## Complete Vote Processing Pipeline

```
┌──────────────────────────────────────────────────────────────────────────┐
│                         STAGE 1: BALLOT SUBMISSION                        │
│                              (API Layer)                                  │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │  api.vote() receives ballot    │
                    │  - Validates structure          │
                    │  - Checks process exists        │
                    │  - Verifies signature           │
                    └───────────────┬────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │ storage.PushPendingBallot()    │
                    │ - Locks address + voteID       │
                    │ - Stores in pending queue      │
                    │ - Status → PENDING             │
                    └───────────────┬────────────────┘
                                    │
                                    ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                      STAGE 2: VOTE VERIFICATION                           │
│                        (BallotProcessor Worker)                           │
└──────────────────────────────────────────────────────────────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │ NextPendingBallot()            │
                    │ - Fetches unreserved ballot    │
                    │ - Creates reservation          │
                    └───────────────┬────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │ processBallot()                │
                    │ - Validates ballot structure   │
                    │ - Prepares circuit inputs      │
                    │ - Generates ZK proof (BLS12377)│
                    │ - Verifies proof locally       │
                    └───────────────┬────────────────┘
                                    │
                    ┌───────────────▼────────────────┐
                    │     Proof Valid?               │
                    └───────┬────────────┬───────────┘
                           YES          NO
                            │            │
                            │            ▼
                            │   ┌────────────────────┐
                            │   │ RemovePendingBallot│
                            │   │ - Delete ballot    │
                            │   │ - Release locks    │
                            │   │ - Status → ERROR   │
                            │   └────────────────────┘
                            │
                            ▼
                    ┌────────────────────┐
                    │ MarkBallotVerified │
                    │ - Move to verified │
                    │   queue            │
                    │ - Status → VERIFIED│
                    └──────────┬─────────┘
                               │
                               ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                      STAGE 3: PROOF AGGREGATION                           │
│                      (AggregateProcessor Worker)                          │
└──────────────────────────────────────────────────────────────────────────┘
                               │
                ┌──────────────▼─────────────────┐
                │ Batch Ready?                   │
                │ - VotesPerBatch ballots OR     │
                │ - Time window elapsed          │
                └──────────────┬─────────────────┘
                               │
                ┌──────────────▼─────────────────┐
                │ PullVerifiedBallots()          │
                │ - Fetch up to VotesPerBatch    │
                │ - Unique addresses only        │
                │ - Create reservations          │
                └──────────────┬─────────────────┘
                               │
                ┌──────────────▼─────────────────┐
                │ collectAggregationBatchInputs()│
                │ - Validate each ballot         │
                │ - Check for duplicates         │
                │ - Verify subgroup membership   │
                │ - Re-verify proofs             │
                │ - Skip invalid/duplicate votes │
                └──────────────┬─────────────────┘
                               │
                ┌──────────────▼─────────────────┐
                │ aggregateBatch()               │
                │ - Fill with dummy proofs       │
                │ - Generate aggregate proof     │
                │   (BW6-761 recursive)          │
                └──────────────┬─────────────────┘
                               │
                ┌──────────────▼─────────────────┐
                │     Proof Valid?               │
                └───────┬──────────────┬─────────┘
                       YES            NO
                        │              │
                        │              ▼
                        │   ┌──────────────────────────┐
                        │   │ ReleaseVerifiedBallot    │
                        │   │ Reservations()           │
                        │   │ - Release for retry OR   │
                        │   │ MarkVerifiedBallotsFailed│
                        │   │ - Status → ERROR         │
                        │   └──────────────────────────┘
                        │
                        ▼
                ┌────────────────────┐
                │ PushAggregatorBatch│
                │ + MarkVerifiedBallots│
                │   Done()           │
                │ - Store batch      │
                │ - Release locks    │
                │ - Status → AGGREGATED│
                └──────────┬─────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                     STAGE 4: STATE TRANSITION                             │
│                  (StateTransitionProcessor Worker)                        │
└──────────────────────────────────────────────────────────────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ NextAggregatorBatch()      │
                │ - Fetch ready batch        │
                │ - Check no pending txs     │
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ latestProcessState()       │
                │ - Load current state       │
                │ - Get root hash            │
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ reencryptVotes()           │
                │ - Generate k seed          │
                │ - Reencrypt ballots        │
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ processCensusProofs()      │
                │ - Load census tree/CSP     │
                │ - Generate proofs          │
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ processStateTransitionBatch│
                │ - Build witness            │
                │ - Generate KZG commitment  │
                │ - Generate proof (BN254)   │
                │ - Retry on constraint error│
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │     Proof Valid?           │
                └───────┬──────────┬─────────┘
                       YES        NO
                        │          │
                        │          ▼
                        │   ┌──────────────────────┐
                        │   │ MarkAggregatorBatch  │
                        │   │ Failed()             │
                        │   │ - Status → ERROR     │
                        │   └──────────────────────┘
                        │
                        ▼
                ┌────────────────────┐
                │ PushStateTransition│
                │ Batch()            │
                │ + MarkAggregatorBatch│
                │   Done()           │
                │ - Store proof      │
                │ - Store blob       │
                │ - Status → PROCESSED│
                └──────────┬─────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                    STAGE 5: ON-CHAIN SETTLEMENT                           │
│                     (OnchainProcessor Worker)                             │
└──────────────────────────────────────────────────────────────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ NextStateTransitionBatch() │
                │ - Fetch ready batch        │
                └──────────┬─────────────────┘
                           │
                ┌──────────▼─────────────────┐
                │ Check State Root Match     │
                │ - Compare local vs remote  │
                └───────┬──────────┬─────────┘
                       YES        NO
                        │          │
                        │          ▼
                        │   ┌──────────────────────┐
                        │   │ MarkStateTransition  │
                        │   │ BatchOutdated()      │
                        │   │ - Ballots return to  │
                        │   │   AGGREGATED status  │
                        │   │ - Retry with new root│
                        │   └──────────────────────┘
                        │
                        ▼
                ┌────────────────────┐
                │ pushTransitionTo   │
                │ Contract()         │
                │ - Convert to       │
                │   Solidity format  │
                │ - Simulate tx      │
                │ - Submit with blob │
                │ - Wait for mining  │
                └──────────┬─────────┘
                           │
                ┌──────────▼─────────────────┐
                │     TX Mined?              │
                └───────┬──────────┬─────────┘
                       YES        NO
                        │          │
                        │          ▼
                        │   ┌──────────────────────┐
                        │   │ MarkStateTransition  │
                        │   │ BatchFailed()        │
                        │   │ - Status → ERROR     │
                        │   └──────────────────────┘
                        │
                        ▼
                ┌────────────────────┐
                │ MarkStateTransition│
                │ BatchDone()        │
                │ - Status → SETTLED │
                │ - IMMUTABLE STATE  │
                └────────────────────┘
```

## Storage Queues and Reservations

### Queue Flow
```
┌─────────────────┐
│ Pending Ballots │ ← PushPendingBallot()
│   (ballotPrefix)│
└────────┬────────┘
         │ NextPendingBallot() + reservation
         ▼
┌──────────────────┐
│ Verified Ballots │ ← MarkBallotVerified()
│(verifiedBallot   │
│     Prefix)      │
└────────┬─────────┘
         │ PullVerifiedBallots() + reservation
         ▼
┌──────────────────┐
│ Aggregator Batch │ ← PushAggregatorBatch()
│(aggregatorBatch  │
│     Prefix)      │
└────────┬─────────┘
         │ NextAggregatorBatch()
         ▼
┌──────────────────┐
│ State Transition │ ← PushStateTransitionBatch()
│     Batch        │
│(stateTransition  │
│  BatchPrefix)    │
└────────┬─────────┘
         │ NextStateTransitionBatch()
         ▼
┌──────────────────┐
│   Blockchain     │ ← On-chain settlement
└──────────────────┘
```

### Reservation System
- **Purpose**: Prevent concurrent processing of same ballot
- **Mechanism**: Separate reservation prefix for each queue
- **Lifecycle**: Created on fetch, removed on completion/failure
- **Recovery**: Released on error to allow retry

## Error Handling Paths

### 1. Invalid Ballot Structure
```
API → Validation fails → Return error to client
Status: Never created
```

### 2. Vote Proof Generation Failure
```
BallotProcessor → processBallot() fails
→ RemovePendingBallot()
→ Release locks
→ Status: ERROR
```

### 3. Duplicate VoteID
```
AggregateProcessor → collectAggregationBatchInputs()
→ Skip ballot (already in state)
→ MarkVerifiedBallotsFailed()
→ Status: ERROR
```

### 4. Duplicate Address in Batch
```
AggregateProcessor → PullVerifiedBallots()
→ Skip duplicate addresses (unique per batch)
→ Ballot remains in verified queue for next batch
```

### 5. Max Voters Reached
```
AggregateProcessor → Check maxVotersReached
→ Skip if not overwrite
→ MarkVerifiedBallotsFailed()
→ Status: ERROR
```

### 6. Aggregation Proof Failure
```
AggregateProcessor → aggregateBatch() fails
→ ReleaseVerifiedBallotReservations() (for retry)
OR MarkVerifiedBallotsFailed() (if invalid)
→ Status: ERROR (if marked failed)
```

### 7. State Transition Proof Failure
```
StateTransitionProcessor → processStateTransitionBatch() fails
→ MarkAggregatorBatchFailed()
→ Status: ERROR
```

### 8. State Root Mismatch
```
OnchainProcessor → Check remote state root
→ Local ≠ Remote
→ MarkStateTransitionBatchOutdated()
→ Ballots return to AGGREGATED status
→ New state transition generated with correct root
```

### 9. On-Chain Transaction Failure
```
OnchainProcessor → pushTransitionToContract() fails
→ Callback with error
→ MarkStateTransitionBatchFailed()
→ Status: ERROR
```

### 10. Process Timeout
```
Cleanup → MarkVoteIDsAsTimeout()
→ All non-SETTLED votes → TIMEOUT
→ SETTLED votes remain unchanged (immutable)
```

## Status Transition Rules

### Valid Transitions
```
PENDING    → VERIFIED
VERIFIED   → AGGREGATED
AGGREGATED → PROCESSED
PROCESSED  → SETTLED
PROCESSED  → AGGREGATED (rollback on state root mismatch)

Any state  → ERROR (except SETTLED)
Any state  → TIMEOUT (except SETTLED)
```

### Immutable States
- **SETTLED**: Final state, cannot be changed
- **ERROR**: Terminal state (no recovery)
- **TIMEOUT**: Terminal state (no recovery)

### Protected Transitions
- Cannot transition FROM SETTLED (enforced in `setVoteIDStatus()`)
- SETTLED can only be reached FROM PROCESSED
- Backward transitions log warnings but are allowed (for recovery)

## Processor Responsibilities

### BallotProcessor
- **Tick Interval**: 1 second (continuous when ballots available)
- **Concurrency**: Single worker with lock
- **Function**: Generate individual vote ZK proofs
- **Proof Type**: BLS12-377 (vote verifier circuit)

### AggregateProcessor
- **Tick Interval**: Configurable (default: frequent)
- **Batch Trigger**: VotesPerBatch OR time window elapsed
- **Concurrency**: Single worker with lock
- **Function**: Aggregate multiple vote proofs into one
- **Proof Type**: BW6-761 (recursive aggregator circuit)

### StateTransitionProcessor
- **Tick Interval**: 1 second
- **Concurrency**: Single worker with lock
- **Function**: Update process state with aggregated votes
- **Proof Type**: BN254 (state transition circuit)
- **Special**: Includes KZG commitment for blob data

### OnchainProcessor
- **Tick Interval**: 10 seconds (state transitions), 10 seconds (results)
- **Concurrency**: Async callbacks for tx mining
- **Function**: Submit proofs to blockchain
- **Special**: Handles blob transactions (EIP-4844/7594)

## Lock Management

### Address Locks
- **Purpose**: Prevent concurrent votes from same address
- **Scope**: Per process ID
- **Lifecycle**: Acquired on `PushPendingBallot()`, released on completion/error
- **Storage**: In-memory sync.Map

### VoteID Locks
- **Purpose**: Prevent duplicate vote processing
- **Scope**: Global (across all processes)
- **Lifecycle**: Acquired on `PushPendingBallot()`, released on completion/error
- **Storage**: In-memory sync.Map

### Reservation Locks
- **Purpose**: Prevent concurrent processing of same ballot by workers
- **Scope**: Per queue (pending, verified)
- **Lifecycle**: Created on fetch, removed on completion/failure
- **Storage**: Database with reservation prefix

## Key Invariants

1. **Unique VoteID**: Each voteID can only be processed once
2. **Unique Address per Batch**: Aggregation batches contain unique addresses
3. **State Root Consistency**: State transitions must match on-chain root
4. **SETTLED Immutability**: Once SETTLED, status cannot change
5. **Lock Ordering**: Address lock → VoteID lock (prevents deadlock)
6. **Reservation Cleanup**: Always released on error to prevent stuck ballots

## Performance Considerations

### Batch Sizing
- **VotesPerBatch**: Fixed at 8 (circuit constraint)
- **Pull Multiplier**: 2x for aggregation (handles skips)

### Time Windows
- **Aggregation**: Configurable window to batch partial sets
- **State Transition**: Immediate processing when batch ready
- **On-Chain**: 30-minute timeout for tx mining

### Proof Generation
- **CPU/GPU**: Configurable prover backend
- **Parallelism**: Single worker per stage (prevents resource contention)
- **Retry**: State transition retries once on constraint errors

## Monitoring Points

### Critical Metrics
1. Ballots in each queue (pending, verified, aggregated, processed)
2. Processing time per stage
3. Error rates by type
4. Lock contention
5. Reservation timeouts
6. On-chain gas costs
7. Proof generation failures

### Health Checks
1. No stuck ballots (reservations without progress)
2. State root consistency
3. Lock cleanup (no orphaned locks)
4. Queue depth within limits
5. Worker liveness

## Recovery Scenarios

### Worker Crash
- Reservations remain in database
- Next worker iteration skips reserved items
- Manual cleanup may be needed for stuck reservations

### Database Corruption
- Vote ID status provides recovery point
- Can rebuild queues from status
- SETTLED votes are safe (immutable)

### State Root Divergence
- Automatic detection on on-chain submission
- Batch marked outdated
- Ballots return to AGGREGATED for retry
- New state transition generated

### Process Timeout
- Cleanup marks non-SETTLED votes as TIMEOUT
- SETTLED votes remain unchanged
- Prevents indefinite resource holding
