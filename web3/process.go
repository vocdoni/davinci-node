package web3

import (
	"context"
	"fmt"
	"time"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	npbindings "github.com/vocdoni/davinci-contracts/golang-types"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// CreateProcess creates a new process in the ProcessRegistry contract.
// It returns the process ID and the transaction hash.
func (c *Contracts) CreateProcess(process *types.Process) (types.ProcessID, *common.Hash, error) {
	txOpts, err := c.authTransactOpts()
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create transact options: %w", err)
	}

	// get the next process ID from the contract before creating the process to
	// get the correct ID for the process that will be created
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	pid, err := c.processes.GetNextProcessId(&bind.CallOpts{Context: ctx}, c.AccountAddress())
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to get next process ID: %w", err)
	}

	p := process2ContractProcess(process)
	tx, err := c.processes.NewProcess(
		txOpts,
		p.Status,
		p.StartTime,
		p.Duration,
		p.MaxVoters,
		p.BallotMode,
		p.Census,
		p.MetadataURI,
		p.EncryptionKey,
		p.LatestStateRoot,
	)
	if err != nil {
		return types.ProcessID{}, nil, fmt.Errorf("failed to create process: %w", err)
	}
	hash := tx.Hash()
	return types.ProcessID(pid), &hash, nil
}

// Process returns the process with the given ID from the ProcessRegistry
// contract.
func (c *Contracts) Process(processID types.ProcessID) (*types.Process, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()

	p, err := c.processes.GetProcess(&bind.CallOpts{Context: ctx}, processID)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}

	process, err := contractProcess2Process(p)
	if err != nil {
		return nil, err
	}
	process.ID = &processID
	return process, nil
}

// NextProcessID returns the next process ID that will be created in the
// ProcessRegistry contract for the given address.
func (c *Contracts) NextProcessID(address common.Address) (types.ProcessID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()

	pid, err := c.processes.GetNextProcessId(&bind.CallOpts{Context: ctx}, address)
	if err != nil {
		return types.ProcessID{}, fmt.Errorf("failed to get next process ID: %w", err)
	}
	processID := types.ProcessID(pid)
	if !processID.IsValid() {
		return types.ProcessID{}, fmt.Errorf("invalid process ID: %s", processID.String())
	}
	return processID, nil
}

// StateRoot returns the state root of the process with the given ID. It
// returns an error if the process does not exist or if there is an issue with
// the contract call.
func (c *Contracts) StateRoot(processID types.ProcessID) (*types.BigInt, error) {
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
func (c *Contracts) sendProcessTransition(processID types.ProcessID, proof, inputs []byte, blobsSidecar *types.BlobTxSidecar) (types.HexBytes, *common.Hash, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
	defer cancel()
	// Prepare the ABI for packing the data
	processABI, err := c.ProcessRegistryABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get process registry ABI: %w", err)
	}

	// Use transaction manager for automatic nonce management
	var sentTx *gethtypes.Transaction
	txID, txHash, err := c.txManager.SendTx(ctx, func(nonce uint64) (*gethtypes.Transaction, error) {
		internalCtx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
		defer cancel()
		// Build the transaction based on whether blobs are provided
		switch blobsSidecar {
		case nil: // Regular transaction
			data, err := processABI.Pack("submitStateTransition", processID, proof, inputs)
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
				[]any{processID, proof, inputs},
				blobsSidecar,
				nonce,
			)
			return sentTx, err
		}
	})
	// If blob transaction sent successfully, store sidecar for recovery
	if err == nil && sentTx != nil && sentTx.BlobTxSidecar() != nil {
		if err := c.txManager.TrackBlobTxWithSidecar(sentTx); err != nil {
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
	processID types.ProcessID,
	proof, inputs []byte,
	blobsSidecar *types.BlobTxSidecar,
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
func (c *Contracts) sendProcessResults(processID types.ProcessID, proof, inputs []byte) (types.HexBytes, *common.Hash, error) {
	// If the transaction manager is not available, return an error
	if c.txManager == nil {
		return nil, nil, fmt.Errorf("transaction manager not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), web3WaitTimeout)
	defer cancel()
	// Prepare the ABI for packing the data
	processABI, err := c.ProcessRegistryABI()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get process registry ABI: %w", err)
	}
	// Pack the data for the setProcessResults function
	data, err := processABI.Pack("setProcessResults", processID, proof, inputs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to pack data: %w", err)
	}
	// Use transaction manager for automatic nonce management
	return c.txManager.SendTx(ctx, func(nonce uint64) (*gethtypes.Transaction, error) {
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
	processID types.ProcessID,
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
func (c *Contracts) SetProcessStatus(processID types.ProcessID, status types.ProcessStatus) (*common.Hash, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	autOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	autOpts.Context = ctx
	tx, err := c.processes.SetProcessStatus(autOpts, processID, uint8(status))
	if err != nil {
		return nil, fmt.Errorf("failed to set process status: %w", err)
	}
	hash := tx.Hash()
	return &hash, nil
}

// SetProcessMaxVoters sets the maximum number of voters for the process with
// the given ID in the ProcessRegistry contract. It returns the transaction
// hash of the update, or an error if the update fails.
func (c *Contracts) SetProcessMaxVoters(processID types.ProcessID, maxVoters *types.BigInt) (*common.Hash, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	autOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	autOpts.Context = ctx
	tx, err := c.processes.SetProcessMaxVoters(autOpts, processID, maxVoters.MathBigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to set process max voters: %w", err)
	}
	hash := tx.Hash()
	return &hash, nil
}

// SetProcessCensus sets the census of the process with the given ID in the
// ProcessRegistry contract. It returns the transaction hash of the census
// update, or an error if the update fails.
func (c *Contracts) SetProcessCensus(processID types.ProcessID, census types.Census) (*common.Hash, error) {
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	autOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	autOpts.Context = ctx

	var newCensusRoot [32]byte
	copy(newCensusRoot[:], census.CensusRoot)
	tx, err := c.processes.SetProcessCensus(autOpts, processID, npbindings.IProcessRegistryCensus{
		CensusRoot:   newCensusRoot,
		CensusURI:    census.CensusURI,
		CensusOrigin: uint8(census.CensusOrigin),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to set process census: %w", err)
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
					log.Debugw("failed to filter process created, retrying", "error", err)
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
					process, err := c.Process(iter.Event.ProcessId)
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

// MonitorProcessChanges monitors changes to processes by polling the
// ProcessRegistry contract every interval. It applies the provided filter
// functions to detect specific types of changes. It returns a channel that
// emits ProcessWithChanges objects representing the detected changes.
func (c *Contracts) MonitorProcessChanges(
	ctx context.Context,
	interval time.Duration,
	retries int,
	filters ...types.Web3FilterFn,
) (<-chan *types.ProcessWithChanges, error) {
	// Create the channel to emit processes changes and run the monitoring in
	// background
	updatedProcChan := make(chan *types.ProcessWithChanges)
	go func() {
		defer close(updatedProcChan)
		// Create a ticker to apply the filters at the specified interval
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Infow("exiting monitor process changes")
				return
			case <-ticker.C:
				// Calculate the block range to query (from the last processed
				// block to the current block)
				end := c.CurrentBlock()
				// Get the last processed block for process changes using a
				// dedicated lock to avoid race conditions
				c.watchBlockMutex.RLock()
				start := c.lastWatchProcessChangesBlock
				c.watchBlockMutex.RUnlock()
				// Skip if there are no new blocks to process
				if end <= start {
					continue
				}
				// Iterate over each filter function
				for _, filter := range filters {
					// Retry the filter function up to the specified number of retries
					for range retries {
						// Call the filter function with a new context
						ctxQuery, cancel := context.WithTimeout(ctx, web3QueryTimeout)
						err := filter(ctxQuery, start, end, updatedProcChan)
						cancel()
						// If the filter function succeeds, break out of the retry loop
						if err == nil {
							break
						}

					}
				}
				// Update the last processed block after processing all filters
				c.watchBlockMutex.Lock()
				c.lastWatchProcessChangesBlock = end
				c.watchBlockMutex.Unlock()
			}
		}
	}()
	return updatedProcChan, nil
}
