package web3

import (
	"context"
	"fmt"
	"math/big"
	"time"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// contractProcess is a mirror of the on-chain process tuple constructed
// with the auto-generated bindings.
type contractProcess struct {
	Status               uint8
	OrganizationId       common.Address
	EncryptionKey        npbindings.IProcessRegistryEncryptionKey
	LatestStateRoot      *big.Int
	StartTime            *big.Int
	Duration             *big.Int
	MetadataURI          string
	BallotMode           npbindings.IProcessRegistryBallotMode
	Census               npbindings.IProcessRegistryCensus
	VoteCount            *big.Int
	VoteOverwrittenCount *big.Int
	Result               []*big.Int
}

// CreateProcess creates a new process in the ProcessRegistry contract.
// It returns the process ID and the transaction hash.
func (c *Contracts) CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error) {
	txOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transact options: %w", err)
	}

	// get the next process ID from the contract before creating the process to
	// get the correct ID for the process that will be created
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	pid, err := c.processes.GetNextProcessId(&bind.CallOpts{Context: ctx}, c.AccountAddress())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get next process ID: %w", err)
	}
	pidDecoded := &types.ProcessID{}
	pidDecoded.SetBytes(pid[:])

	p := process2ContractProcess(process)
	tx, err := c.processes.NewProcess(
		txOpts,
		p.Status,
		p.StartTime,
		p.Duration,
		p.BallotMode,
		p.Census,
		p.MetadataURI,
		p.EncryptionKey,
		p.LatestStateRoot,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create process: %w", err)
	}
	hash := tx.Hash()
	return pidDecoded, &hash, nil
}

// Process returns the process with the given ID from the ProcessRegistry
// contract.
func (c *Contracts) Process(processID []byte) (*types.Process, error) {
	var pid [32]byte
	copy(pid[:], processID)

	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()

	p, err := c.processes.GetProcess(&bind.CallOpts{Context: ctx}, pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}

	process, err := contractProcess2Process(&contractProcess{
		Status:               p.Status,
		OrganizationId:       p.OrganizationId,
		EncryptionKey:        p.EncryptionKey,
		LatestStateRoot:      p.LatestStateRoot,
		StartTime:            p.StartTime,
		Duration:             p.Duration,
		MetadataURI:          p.MetadataURI,
		BallotMode:           p.BallotMode,
		Census:               p.Census,
		VoteCount:            p.VoteCount,
		VoteOverwrittenCount: p.VoteOverwriteCount,
		Result:               p.Result,
	})
	if err != nil {
		return nil, err
	}
	process.ID = processID
	return process, nil
}

// NextProcessID returns the next process ID that will be created in the
// ProcessRegistry contract for the given address.
func (c *Contracts) NextProcessID(address common.Address) (*types.ProcessID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()

	pid, err := c.processes.GetNextProcessId(&bind.CallOpts{Context: ctx}, address)
	if err != nil {
		return nil, fmt.Errorf("failed to get next process ID: %w", err)
	}
	pidDecoded := &types.ProcessID{}
	pidDecoded.SetBytes(pid[:])
	if !pidDecoded.IsValid() {
		return nil, fmt.Errorf("invalid process ID: %s", pidDecoded.String())
	}
	return pidDecoded, nil
}

// StateRoot returns the state root of the process with the given ID. It
// returns an error if the process does not exist or if there is an issue with
// the contract call.
func (c *Contracts) StateRoot(processID []byte) (*types.BigInt, error) {
	process, err := c.Process(processID)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	return process.StateRoot, nil
}

// sendProcessTransition submits a state transition for the process with the
// given ID. It verifies that the old root matches the current state root of
// the process. It returns the transaction hash of the state transition
// submission, or an error if the submission fails. The tx hash can be used to
// track the status of the transaction on the blockchain.
func (c *Contracts) sendProcessTransition(processID types.HexBytes, proof, inputs []byte, blobsSidecar *gtypes.BlobTxSidecar) (types.HexBytes, *common.Hash, error) {
	// Copy processID into a fixed-size array to match the contract's expected
	// type
	var pid [32]byte
	copy(pid[:], processID)
	ctx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
	defer cancel()
	// Prepare the ABI for packing the data
	processABI, err := c.ProcessRegistryABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get process registry ABI: %w", err)
	}

	// Use transaction manager for automatic nonce management
	var sentTx *gtypes.Transaction
	txID, txHash, err := c.txManager.SendTx(ctx, func(nonce uint64) (*gtypes.Transaction, error) {
		internalCtx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
		defer cancel()
		// Build the transaction based on whether blobs are provided
		switch blobsSidecar {
		case nil: // Regular transaction
			data, err := processABI.Pack("submitStateTransition", pid, proof, inputs)
			if err != nil {
				return nil, fmt.Errorf("failed to pack data: %w", err)
			}
			// No blobs so we dont not need to track sidecar, sentTx will be nil
			return c.txManager.BuildDynamicFeeTx(internalCtx, c.ContractsAddresses.ProcessRegistry, data, nonce)
		default: // Blob transaction
			// Store tx in sentTx for tracking sidecar later
			sentTx, err = c.NewEIP4844TransactionWithNonce(
				internalCtx,
				c.ContractsAddresses.ProcessRegistry,
				processABI,
				"submitStateTransition",
				[]any{pid, proof, inputs},
				blobsSidecar,
				nonce,
			)
			return sentTx, err
		}
	})
	// If blob transaction sent successfully, store sidecar for recovery
	if err == nil && blobsSidecar != nil && sentTx != nil {
		if err := c.txManager.TrackBlobTxWithSidecar(sentTx, blobsSidecar); err != nil {
			log.Warnw("failed to track blob sidecar for recovery",
				"error", err,
				"hash", txHash.Hex(),
				"txID", txID.String())
		} else {
			log.Infow("blob sidecar tracked for stuck transaction recovery",
				"hash", txHash.Hex(),
				"txID", txID.String(),
				"blobCount", len(blobsSidecar.Blobs))
		}
	}
	log.Infow("state transition submitted, wait to be mined",
		"processID", processID.String())
	return txID, txHash, err
}

// SetProcessTransition submits a state transition for the process with the
// given ID and waits for the transaction to be mined. Once mined or the timeout
// is reached, it calls the optional callback with the result of the operation.
// It returns an error if the submission fails.
func (c *Contracts) SetProcessTransition(
	processID types.HexBytes,
	proof, inputs []byte,
	blobsSidecar *gtypes.BlobTxSidecar,
	timeout time.Duration,
	callback ...func(error),
) error {
	txID, txHash, err := c.sendProcessTransition(processID, proof, inputs, blobsSidecar)
	if err != nil {
		return fmt.Errorf("failed to set process transition: %w", err)
	}
	log.Infow("waiting for state transition to be mined",
		"hash", txHash.Hex(),
		"txID", txID.String(),
		"processID", processID.String())
	return c.txManager.WaitTxByID(txID, timeout, callback...)
}

// sendProcessResults sets the results of the process with the given ID in the
// ProcessRegistry contract. It returns the transaction ID and hash of the
// results submission, or an error if the submission fails.
func (c *Contracts) sendProcessResults(processID types.HexBytes, proof, inputs []byte) (types.HexBytes, *common.Hash, error) {
	// If the transaction manager is not available, return an error
	if c.txManager == nil {
		return nil, nil, fmt.Errorf("transaction manager not initialized")
	}
	// Copy processID into a fixed-size array to match the contract's expected
	// type
	var pid [32]byte
	copy(pid[:], processID)
	ctx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
	defer cancel()
	// Prepare the ABI for packing the data
	processABI, err := c.ProcessRegistryABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get process registry ABI: %w", err)
	}
	// Pack the data for the setProcessResults function
	data, err := processABI.Pack("setProcessResults", pid, proof, inputs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack data: %w", err)
	}
	// Use transaction manager for automatic nonce management
	return c.txManager.SendTx(ctx, func(nonce uint64) (*gtypes.Transaction, error) {
		internalCtx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
		defer cancel()
		// Results are always regular transactions (no blobs)
		return c.txManager.BuildDynamicFeeTx(internalCtx, c.ContractsAddresses.ProcessRegistry, data, nonce)
	})
}

// SetProcessResults sets the results of the process with the given ID in the
// ProcessRegistry contract and waits for the transaction to be mined. Once
// mined or the timeout is reached, it calls the optional callback with the
// result of the operation. It returns an error if the submission fails.
func (c *Contracts) SetProcessResults(
	processID types.HexBytes,
	proof, inputs []byte,
	timeout time.Duration, callback ...func(error),
) error {
	txID, txHash, err := c.sendProcessResults(processID, proof, inputs)
	if err != nil {
		return fmt.Errorf("failed to set process results: %w", err)
	}
	log.Infow("waiting for process results to be mined",
		"hash", txHash.Hex(),
		"txID", txID.String(),
		"processID", processID.String())
	return c.txManager.WaitTxByID(txID, timeout, callback...)
}

// SetProcessStatus sets the status of the process with the given ID in the
// ProcessRegistry contract. It returns the transaction hash of the status
// update, or an error if the update fails.
func (c *Contracts) SetProcessStatus(processID types.HexBytes, status types.ProcessStatus) (*common.Hash, error) {
	var pid [32]byte
	copy(pid[:], processID)
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	autOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	autOpts.Context = ctx
	tx, err := c.processes.SetProcessStatus(autOpts, pid, uint8(status))
	if err != nil {
		return nil, fmt.Errorf("failed to set process status: %w", err)
	}
	hash := tx.Hash()
	return &hash, nil
}

// MonitorProcessCreation monitors the creation of new processes by polling the
// ProcessRegistry contract every interval.
func (c *Contracts) MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error) {
	ch := make(chan *types.Process)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Infow("exiting monitor process creation")
				return
			case <-ticker.C:
				end := c.CurrentBlock()
				// Use dedicated cursor for process creation events to avoid race conditions
				c.watchBlockMutex.RLock()
				start := c.lastWatchProcessCreationBlock
				c.watchBlockMutex.RUnlock()
				if end <= start {
					continue
				}
				ctxQuery, cancel := context.WithTimeout(ctx, web3QueryTimeout)
				iter, err := c.processes.FilterProcessCreated(&bind.FilterOpts{Start: start, End: &end, Context: ctxQuery}, nil, nil)
				cancel()
				if err != nil || iter == nil {
					log.Debugw("failed to filter process created, retrying", "err", err)
					continue
				}
				// Update cursor after successful query
				c.watchBlockMutex.Lock()
				c.lastWatchProcessCreationBlock = end
				c.watchBlockMutex.Unlock()
				for iter.Next() {
					processID := fmt.Sprintf("%x", iter.Event.ProcessId)
					// Thread-safe check and update of knownProcesses map
					c.knownProcessesMutex.RLock()
					_, exists := c.knownProcesses[processID]
					c.knownProcessesMutex.RUnlock()
					if exists {
						continue
					}
					c.knownProcessesMutex.Lock()
					c.knownProcesses[processID] = struct{}{}
					c.knownProcessesMutex.Unlock()
					process, err := c.Process(iter.Event.ProcessId[:])
					if err != nil {
						log.Errorw(err, "failed to get process while monitoring process creation")
						continue
					}
					ch <- process
				}
			}
		}
	}()
	return ch, nil
}

// MonitorProcessStatusChanges monitors the status changes in processes by
// polling the ProcessRegistry contract every interval. It returns a channel
// that emits processes with the old and new status.
func (c *Contracts) MonitorProcessStatusChanges(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStatusChange, error) {
	updatedProcChan := make(chan *types.ProcessWithStatusChange)
	go func() {
		defer close(updatedProcChan)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Infow("exiting monitor process updates")
				return
			case <-ticker.C:
				end := c.CurrentBlock()
				// Use dedicated cursor for status change events to avoid race conditions
				c.watchBlockMutex.RLock()
				start := c.lastWatchProcessStatusBlock
				c.watchBlockMutex.RUnlock()
				if end <= start {
					continue
				}
				ctxQuery, cancel := context.WithTimeout(ctx, web3QueryTimeout)
				iter, err := c.processes.FilterProcessStatusChanged(&bind.FilterOpts{Start: start, End: &end, Context: ctxQuery}, nil)
				cancel()
				if err != nil || iter == nil {
					log.Debugw("failed to filter process finalized, retrying", "err", err)
					continue
				}
				// Update cursor after successful query
				c.watchBlockMutex.Lock()
				c.lastWatchProcessStatusBlock = end
				c.watchBlockMutex.Unlock()
				for iter.Next() {
					processID := fmt.Sprintf("%x", iter.Event.ProcessId)
					// Thread-safe check of knownProcesses map
					c.knownProcessesMutex.RLock()
					_, exists := c.knownProcesses[processID]
					c.knownProcessesMutex.RUnlock()
					if !exists {
						continue
					}
					process, err := c.Process(iter.Event.ProcessId[:])
					if err != nil {
						log.Errorw(err, "failed to get process while monitoring process status changes")
						continue
					}
					updatedProcChan <- &types.ProcessWithStatusChange{
						Process:   process,
						OldStatus: types.ProcessStatus(iter.Event.OldStatus),
						NewStatus: types.ProcessStatus(iter.Event.NewStatus),
					}
				}
			}
		}
	}()
	return updatedProcChan, nil
}

// MonitorProcessStateRootChange monitors the state root changes in processes
// by polling the ProcessRegistry contract every interval. It returns a channel
// that emits processes with the new state root, vote count, and vote
// overwritten count.
func (c *Contracts) MonitorProcessStateRootChange(ctx context.Context, interval time.Duration) (<-chan *types.ProcessWithStateRootChange, error) {
	updatedProcChan := make(chan *types.ProcessWithStateRootChange)
	go func() {
		defer close(updatedProcChan)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Infow("exiting monitor process updates")
				return
			case <-ticker.C:
				end := c.CurrentBlock()
				// Use dedicated cursor for state root events to prevent concurrent
				// monitors from interfering with each other's block tracking.
				c.watchBlockMutex.RLock()
				start := c.lastWatchProcessStateRootBlock
				c.watchBlockMutex.RUnlock()
				if end <= start {
					continue
				}
				ctxQuery, cancel := context.WithTimeout(ctx, web3QueryTimeout)
				iter, err := c.processes.FilterProcessStateRootUpdated(&bind.FilterOpts{Start: start, End: &end, Context: ctxQuery}, nil, nil)
				cancel()
				if err != nil || iter == nil {
					log.Debugw("failed to filter process finalized, retrying", "err", err)
					continue
				}
				c.watchBlockMutex.Lock()
				c.lastWatchProcessStateRootBlock = end
				c.watchBlockMutex.Unlock()
				for iter.Next() {
					processID := fmt.Sprintf("%x", iter.Event.ProcessId)
					// Check if process is registered in knownProcesses map
					c.knownProcessesMutex.RLock()
					_, exists := c.knownProcesses[processID]
					c.knownProcessesMutex.RUnlock()
					if !exists {
						continue
					}
					process, err := c.Process(iter.Event.ProcessId[:])
					if err != nil {
						log.Errorw(err, "failed to get process while monitoring process status changes")
						continue
					}

					updatedProcChan <- &types.ProcessWithStateRootChange{
						Process:                 process,
						NewStateRoot:            new(types.BigInt).SetBigInt(iter.Event.NewStateRoot),
						NewVoteCount:            process.VoteCount,
						NewVoteOverwrittenCount: process.VoteOverwrittenCount,
					}
				}
			}
		}
	}()
	return updatedProcChan, nil
}

// MonitorProcessCreationBySubscription monitors the creation of new processes
// by subscribing to the ProcessRegistry contract. Requires the web3 rpc
// endpoint to support subscriptions on websockets.
func (c *Contracts) MonitorProcessCreationBySubscription(ctx context.Context) (<-chan *types.Process, error) {
	ch1 := make(chan *npbindings.ProcessRegistryProcessCreated)
	ch2 := make(chan *types.Process)

	sub, err := c.processes.WatchProcessCreated(nil, ch1, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to watch process created: %w", err)
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Warnw("exiting monitor process created")
				sub.Unsubscribe()
				close(ch1)
				close(ch2)
				return
			case <-sub.Err():
				log.Errorw(err, "failed to watch process created")
				close(ch1)
				close(ch2)
				return
			case event := <-ch1:
				go func() {
					var p *types.Process
					var err error
					maxTries := 20
					for {
						// wait for the process to be indexed by web3 providers
						time.Sleep(1 * time.Second)
						p, err = c.Process(event.ProcessId[:])
						if err != nil {
							log.Errorw(err, "failed to get process while monitoring")
							continue
						}
						if p.Status == types.ProcessStatusEnded {
							log.Debugw("process already ended, skipping", "processId", event.ProcessId)
							return // Skip already ended processes
						}
						if p.OrganizationId.Cmp(common.Address{}) != 0 {
							ch2 <- p
							break
						}
						maxTries--
						if maxTries == 0 {
							log.Errorw(fmt.Errorf("max tries reached while monitoring process created"), fmt.Sprintf("processId:%x", event.ProcessId))
							break
						}
					}
				}()
			}
		}
	}()
	return ch2, nil
}

func contractProcess2Process(p *contractProcess) (*types.Process, error) {
	mode := types.BallotMode{
		UniqueValues:   p.BallotMode.UniqueValues,
		CostFromWeight: p.BallotMode.CostFromWeight,
		NumFields:      p.BallotMode.NumFields,
		CostExponent:   p.BallotMode.CostExponent,
		MaxValue:       (*types.BigInt)(p.BallotMode.MaxValue),
		MinValue:       (*types.BigInt)(p.BallotMode.MinValue),
		MaxValueSum:    (*types.BigInt)(p.BallotMode.MaxValueSum),
		MinValueSum:    (*types.BigInt)(p.BallotMode.MinValueSum),
	}
	if err := mode.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ballot mode: %w", err)
	}

	// Validate the census origin
	censusOrigin := types.CensusOrigin(p.Census.CensusOrigin)
	if !censusOrigin.Valid() {
		return nil, fmt.Errorf("invalid census origin: %d", p.Census.CensusOrigin)
	}

	census := types.Census{
		CensusRoot:   p.Census.CensusRoot[:],
		MaxVotes:     (*types.BigInt)(p.Census.MaxVotes),
		CensusURI:    p.Census.CensusURI,
		CensusOrigin: types.CensusOrigin(p.Census.CensusOrigin),
	}

	results := make([]*types.BigInt, len(p.Result))
	for i, r := range p.Result {
		results[i] = (*types.BigInt)(r)
	}

	return &types.Process{
		Status:         types.ProcessStatus(p.Status),
		OrganizationId: p.OrganizationId,
		EncryptionKey: &types.EncryptionKey{
			X: (*types.BigInt)(p.EncryptionKey.X),
			Y: (*types.BigInt)(p.EncryptionKey.Y),
		},
		StateRoot:            (*types.BigInt)(p.LatestStateRoot),
		StartTime:            time.Unix(int64(p.StartTime.Uint64()), 0),
		Duration:             time.Duration(p.Duration.Uint64()) * time.Second,
		MetadataURI:          p.MetadataURI,
		BallotMode:           &mode,
		Census:               &census,
		VoteCount:            (*types.BigInt)(p.VoteCount),
		VoteOverwrittenCount: (*types.BigInt)(p.VoteOverwrittenCount),
		Result:               results,
	}, nil
}

func process2ContractProcess(p *types.Process) contractProcess {
	var prp contractProcess

	prp.Status = uint8(p.Status)
	prp.OrganizationId = p.OrganizationId
	prp.EncryptionKey = npbindings.IProcessRegistryEncryptionKey{
		X: p.EncryptionKey.X.MathBigInt(),
		Y: p.EncryptionKey.Y.MathBigInt(),
	}

	prp.LatestStateRoot = p.StateRoot.MathBigInt()
	prp.StartTime = big.NewInt(p.StartTime.Unix())
	prp.Duration = big.NewInt(int64(p.Duration.Seconds()))
	prp.MetadataURI = p.MetadataURI

	prp.BallotMode = npbindings.IProcessRegistryBallotMode{
		CostFromWeight: p.BallotMode.CostFromWeight,
		UniqueValues:   p.BallotMode.UniqueValues,
		NumFields:      p.BallotMode.NumFields,
		CostExponent:   p.BallotMode.CostExponent,
		MaxValue:       p.BallotMode.MaxValue.MathBigInt(),
		MinValue:       p.BallotMode.MinValue.MathBigInt(),
		MaxValueSum:    p.BallotMode.MaxValueSum.MathBigInt(),
		MinValueSum:    p.BallotMode.MinValueSum.MathBigInt(),
	}

	copy(prp.Census.CensusRoot[:], p.Census.CensusRoot)
	prp.Census.CensusOrigin = uint8(p.Census.CensusOrigin)
	prp.Census.MaxVotes = p.Census.MaxVotes.MathBigInt()
	prp.Census.CensusURI = p.Census.CensusURI
	prp.VoteCount = p.VoteCount.MathBigInt()
	prp.VoteOverwrittenCount = p.VoteOverwrittenCount.MathBigInt()
	if p.Result != nil {
		prp.Result = make([]*big.Int, len(p.Result))
		for i, r := range p.Result {
			prp.Result[i] = r.MathBigInt()
		}
	} else {
		prp.Result = []*big.Int{} // Ensure it's not nil
	}
	return prp
}
