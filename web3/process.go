package web3

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	bindings "github.com/vocdoni/contracts-z/golang-types/non-proxy"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// CreateProcess creates a new process in the ProcessRegistry contract.
// It returns the process ID and the transaction hash.
func (c *Contracts) CreateProcess(process *types.Process) (*types.ProcessID, *common.Hash, error) {
	txOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	pid := types.ProcessID{
		Address: process.OrganizationId,
		Nonce:   txOpts.Nonce.Uint64(),
		ChainID: uint32(c.ChainID),
	}
	pid32 := [32]byte{}
	copy(pid32[:], pid.Marshal())
	p := process2ContractProcess(process)
	tx, err := c.processes.NewProcess(
		txOpts,
		p.Status,
		p.StartTime,
		p.Duration,
		p.BallotMode,
		p.Census,
		p.MetadataURI,
		p.OrganizationId,
		pid32,
		p.EncryptionKey,
		p.LatestStateRoot,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create process: %w", err)
	}
	hash := tx.Hash()
	return &pid, &hash, nil
}

// Process returns the process with the given ID from the ProcessRegistry contract.
func (c *Contracts) Process(processID []byte) (*types.Process, error) {
	var pid [32]byte
	copy(pid[:], processID)
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	process, err := c.processes.GetProcess(&bind.CallOpts{Context: ctx}, pid)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	return contractProcess2Process(&process)
}

func (c *Contracts) SetProcessTransition(processID, oldRoot, newRoot, proof []byte) (*common.Hash, error) {
	process, err := c.Process(processID)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	if !bytes.Equal(process.StateRoot, oldRoot) {
		return nil, fmt.Errorf("process state root mismatch: %x != %x", process.StateRoot, oldRoot)
	}

	var pid [32]byte
	copy(pid[:], processID)
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	var oldRoot32, newRoot32 [32]byte
	copy(oldRoot32[:], oldRoot)
	copy(newRoot32[:], newRoot)
	autOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	autOpts.Context = ctx
	tx, err := c.processes.SubmitStateTransition(autOpts, pid, oldRoot32, newRoot32, proof)
	if err != nil {
		return nil, fmt.Errorf("failed to submit state transition: %w", err)
	}
	hash := tx.Hash()
	return &hash, nil
}

// MonitorProcessCreation monitors the creation of new processes by polling the ProcessRegistry contract every interval.
func (c *Contracts) MonitorProcessCreation(ctx context.Context, interval time.Duration) (<-chan *types.Process, error) {
	ch := make(chan *types.Process)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Warnw("exiting monitor process creation")
				return
			case <-ticker.C:
				end := c.CurrentBlock()
				if end <= c.lastWatchProcessBlock {
					continue
				}
				ctxQuery, cancel := context.WithTimeout(ctx, web3QueryTimeout)
				iter, err := c.processes.FilterProcessCreated(&bind.FilterOpts{Start: c.lastWatchProcessBlock, End: &end, Context: ctxQuery}, nil, nil)
				cancel()
				if err != nil || iter == nil {
					log.Debugw("failed to filter process created, retrying", "err", err)
					continue
				}
				c.lastWatchProcessBlock = end
				for iter.Next() {
					processID := fmt.Sprintf("%x", iter.Event.ProcessID)
					if _, exists := c.knownProcesses[processID]; exists {
						continue
					}
					c.knownProcesses[processID] = struct{}{}
					process, err := c.Process(iter.Event.ProcessID[:])
					if err != nil {
						log.Errorw(err, "failed to get process while monitoring process creation")
						continue
					}
					process.ID = iter.Event.ProcessID[:]
					log.Debugw("new process found", "processId", process.ID, "blockNumber", iter.Event.Raw.BlockNumber)
					ch <- process
				}
			}
		}
	}()
	return ch, nil
}

// MonitorProcessCreationBySubscription monitors the creation of new processes by subscribing to the ProcessRegistry contract.
// Requires the web3 rpc endpoint to support subscriptions on websockets.
func (c *Contracts) MonitorProcessCreationBySubscription(ctx context.Context) (<-chan *types.Process, error) {
	ch1 := make(chan *bindings.ProcessRegistryProcessCreated)
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
						p, err = c.Process(event.ProcessID[:])
						if err != nil {
							log.Errorw(err, "failed to get process while monitoring")
							continue
						}
						if p.OrganizationId.Cmp(common.Address{}) != 0 {
							p.ID = event.ProcessID[:]
							ch2 <- p
							break
						}
						maxTries--
						if maxTries == 0 {
							log.Errorw(fmt.Errorf("max tries reached while monitoring process created"), fmt.Sprintf("processId:%x", event.ProcessID))
							break
						}
					}
				}()
			}
		}
	}()
	return ch2, nil
}

func contractProcess2Process(contractProcess *bindings.ProcessRegistryProcess) (*types.Process, error) {
	mode := types.BallotMode{
		ForceUniqueness: contractProcess.BallotMode.ForceUniqueness,
		CostFromWeight:  contractProcess.BallotMode.CostFromWeight,
		MaxCount:        contractProcess.BallotMode.MaxCount,
		CostExponent:    contractProcess.BallotMode.CostExponent,
		MaxValue:        (*types.BigInt)(contractProcess.BallotMode.MaxValue),
		MinValue:        (*types.BigInt)(contractProcess.BallotMode.MinValue),
		MaxTotalCost:    (*types.BigInt)(contractProcess.BallotMode.MaxTotalCost),
		MinTotalCost:    (*types.BigInt)(contractProcess.BallotMode.MinTotalCost),
	}
	if err := mode.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ballot mode: %w", err)
	}
	census := types.Census{
		CensusRoot:   contractProcess.Census.CensusRoot[:],
		MaxVotes:     (*types.BigInt)(contractProcess.Census.MaxVotes),
		CensusURI:    contractProcess.Census.CensusURI,
		CensusOrigin: contractProcess.Census.CensusOrigin,
	}
	return &types.Process{
		Status:         contractProcess.Status,
		OrganizationId: contractProcess.OrganizationId,
		EncryptionKey: &types.EncryptionKey{
			X: contractProcess.EncryptionKey.X,
			Y: contractProcess.EncryptionKey.Y,
		},
		StateRoot:   contractProcess.LatestStateRoot[:],
		StartTime:   time.Unix(int64(contractProcess.StartTime.Uint64()), 0),
		Duration:    time.Duration(contractProcess.Duration.Uint64()) * time.Second,
		MetadataURI: contractProcess.MetadataURI,
		BallotMode:  &mode,
		Census:      &census,
	}, nil
}

func process2ContractProcess(process *types.Process) *bindings.ProcessRegistryProcess {
	ballotMode := bindings.ProcessRegistryBallotMode{
		ForceUniqueness: process.BallotMode.ForceUniqueness,
		MaxCount:        process.BallotMode.MaxCount,
		CostExponent:    process.BallotMode.CostExponent,
		MaxValue:        process.BallotMode.MaxValue.MathBigInt(),
		MinValue:        process.BallotMode.MinValue.MathBigInt(),
		MaxTotalCost:    process.BallotMode.MaxTotalCost.MathBigInt(),
		MinTotalCost:    process.BallotMode.MinTotalCost.MathBigInt(),
	}
	census := bindings.ProcessRegistryCensus{
		CensusRoot:   [32]byte{},
		MaxVotes:     process.Census.MaxVotes.MathBigInt(),
		CensusURI:    process.Census.CensusURI,
		CensusOrigin: process.Census.CensusOrigin,
	}
	copy(census.CensusRoot[:], process.Census.CensusRoot)
	encryptionKey := bindings.ProcessRegistryEncryptionKey{
		X: process.EncryptionKey.X,
		Y: process.EncryptionKey.Y,
	}
	stateRoot := [32]byte{}
	copy(stateRoot[:], process.StateRoot)
	return &bindings.ProcessRegistryProcess{
		Status:          process.Status,
		OrganizationId:  process.OrganizationId,
		EncryptionKey:   encryptionKey,
		LatestStateRoot: stateRoot,
		StartTime:       big.NewInt(process.StartTime.Unix()),
		Duration:        big.NewInt(int64(process.Duration.Seconds())),
		MetadataURI:     process.MetadataURI,
		BallotMode:      ballotMode,
		Census:          census,
	}
}
