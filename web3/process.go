package web3

import (
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
	defer cancel()

	p, err := c.processes.GetProcess(&bind.CallOpts{Context: ctx}, pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}

	return contractProcess2Process(&ProcessRegistryProcess{
		Status:             p.Status,
		OrganizationId:     p.OrganizationId,
		EncryptionKey:      p.EncryptionKey,
		LatestStateRoot:    p.LatestStateRoot,
		StartTime:          p.StartTime,
		Duration:           p.Duration,
		MetadataURI:        p.MetadataURI,
		BallotMode:         p.BallotMode,
		Census:             p.Census,
		VoteCount:          p.VoteCount,
		VoteOverwriteCount: p.VoteOverwriteCount,
	})
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

// SetProcessTransition submits a state transition for the process with the
// given ID. It verifies that the old root matches the current state root of
// the process. It returns the transaction hash of the state transition
// submission, or an error if the submission fails. The tx hash can be used to
// track the status of the transaction on the blockchain.
func (c *Contracts) SetProcessTransition(processID, proof, input []byte, oldRoot *types.BigInt) (*common.Hash, error) {
	stateRoot, err := c.StateRoot(processID)
	if err != nil {
		return nil, fmt.Errorf("failed to get process: %w", err)
	}
	// if !bytes.Equal(stateRoot, oldRoot) {
	if stateRoot.MathBigInt().Cmp(oldRoot.MathBigInt()) != 0 {
		return nil, fmt.Errorf("process state root mismatch: %x != %x", stateRoot, oldRoot)
	}
	var pid [32]byte
	copy(pid[:], processID)
	ctx, cancel := context.WithTimeout(context.Background(), web3QueryTimeout)
	defer cancel()
	autOpts, err := c.authTransactOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to create transact options: %w", err)
	}
	autOpts.Context = ctx
	tx, err := c.processes.SubmitStateTransition(autOpts, pid, proof, input)
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
				log.Infow("exiting monitor process creation")
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
					processID := fmt.Sprintf("%x", iter.Event.ProcessId)
					if _, exists := c.knownProcesses[processID]; exists {
						continue
					}
					c.knownProcesses[processID] = struct{}{}
					process, err := c.Process(iter.Event.ProcessId[:])
					if err != nil {
						log.Errorw(err, "failed to get process while monitoring process creation")
						continue
					}
					process.ID = iter.Event.ProcessId[:]
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
						p, err = c.Process(event.ProcessId[:])
						if err != nil {
							log.Errorw(err, "failed to get process while monitoring")
							continue
						}
						if p.OrganizationId.Cmp(common.Address{}) != 0 {
							p.ID = event.ProcessId[:]
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

func contractProcess2Process(p *ProcessRegistryProcess) (*types.Process, error) {
	mode := types.BallotMode{
		ForceUniqueness: p.BallotMode.ForceUniqueness,
		CostFromWeight:  p.BallotMode.CostFromWeight,
		MaxCount:        p.BallotMode.MaxCount,
		CostExponent:    p.BallotMode.CostExponent,
		MaxValue:        (*types.BigInt)(p.BallotMode.MaxValue),
		MinValue:        (*types.BigInt)(p.BallotMode.MinValue),
		MaxTotalCost:    (*types.BigInt)(p.BallotMode.MaxTotalCost),
		MinTotalCost:    (*types.BigInt)(p.BallotMode.MinTotalCost),
	}
	if err := mode.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ballot mode: %w", err)
	}

	census := types.Census{
		CensusRoot:   p.Census.CensusRoot[:],
		MaxVotes:     (*types.BigInt)(p.Census.MaxVotes),
		CensusURI:    p.Census.CensusURI,
		CensusOrigin: p.Census.CensusOrigin,
	}

	return &types.Process{
		Status:         p.Status,
		OrganizationId: p.OrganizationId,
		EncryptionKey: &types.EncryptionKey{
			X: p.EncryptionKey.X,
			Y: p.EncryptionKey.Y,
		},
		StateRoot:          (*types.BigInt)(p.LatestStateRoot),
		StartTime:          time.Unix(int64(p.StartTime.Uint64()), 0),
		Duration:           time.Duration(p.Duration.Uint64()) * time.Second,
		MetadataURI:        p.MetadataURI,
		BallotMode:         &mode,
		Census:             &census,
		VoteCount:          (*types.BigInt)(p.VoteCount),
		VoteOverwriteCount: (*types.BigInt)(p.VoteOverwriteCount),
	}, nil
}

// ProcessRegistryProcess is a mirror of the on-chain process tuple constructed with the auto-generated bindings
type ProcessRegistryProcess struct {
	Status             uint8
	OrganizationId     common.Address
	EncryptionKey      bindings.IProcessRegistryEncryptionKey
	LatestStateRoot    *big.Int
	StartTime          *big.Int
	Duration           *big.Int
	MetadataURI        string
	BallotMode         bindings.IProcessRegistryBallotMode
	Census             bindings.IProcessRegistryCensus
	VoteCount          *big.Int
	VoteOverwriteCount *big.Int
}

func process2ContractProcess(p *types.Process) ProcessRegistryProcess {
	var prp ProcessRegistryProcess

	prp.Status = p.Status
	prp.OrganizationId = p.OrganizationId
	prp.EncryptionKey = bindings.IProcessRegistryEncryptionKey{X: p.EncryptionKey.X, Y: p.EncryptionKey.Y}

	prp.LatestStateRoot = p.StateRoot.MathBigInt()
	prp.StartTime = big.NewInt(p.StartTime.Unix())
	prp.Duration = big.NewInt(int64(p.Duration.Seconds()))
	prp.MetadataURI = p.MetadataURI

	prp.BallotMode = bindings.IProcessRegistryBallotMode{
		CostFromWeight:  p.BallotMode.CostFromWeight,
		ForceUniqueness: p.BallotMode.ForceUniqueness,
		MaxCount:        p.BallotMode.MaxCount,
		CostExponent:    p.BallotMode.CostExponent,
		MaxValue:        p.BallotMode.MaxValue.MathBigInt(),
		MinValue:        p.BallotMode.MinValue.MathBigInt(),
		MaxTotalCost:    p.BallotMode.MaxTotalCost.MathBigInt(),
		MinTotalCost:    p.BallotMode.MinTotalCost.MathBigInt(),
	}

	copy(prp.Census.CensusRoot[:], p.Census.CensusRoot)
	prp.Census.CensusOrigin = p.Census.CensusOrigin
	prp.Census.MaxVotes = p.Census.MaxVotes.MathBigInt()
	prp.Census.CensusURI = p.Census.CensusURI
	prp.VoteCount = p.VoteCount.MathBigInt()
	prp.VoteOverwriteCount = p.VoteOverwriteCount.MathBigInt()

	return prp
}
