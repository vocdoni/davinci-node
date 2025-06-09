package sequencer

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/consensys/gnark/backend"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/backend/solidity"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/results"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
)

const (
	failbackMaxValue = 2 << 24 // 2^24
)

// finalizer is responsible for finalizing processes.
type finalizer struct {
	stg        *storage.Storage
	stateDB    db.Database
	circuits   *internalCircuits // Internal circuit artifacts for proof generation and verification
	prover     ProverFunc        // Function for generating zero-knowledge proofs
	OndemandCh chan *types.ProcessID
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	lock       sync.Mutex // Mutex to ensure that only one process results calculation is running at a time
}

// New creates a new Finalizer instance.
func newFinalizer(stg *storage.Storage, stateDB db.Database, ca *internalCircuits, prover ProverFunc) *finalizer {
	// Default prover function if none is provided
	if prover == nil {
		prover = DefaultProver
	}
	// We'll create the context in Start() now to avoid premature cancellation
	return &finalizer{
		stg:        stg,
		stateDB:    stateDB,
		circuits:   ca,
		prover:     prover,
		OndemandCh: make(chan *types.ProcessID, 10), // Use buffered channel to prevent blocking
	}
}

// Start starts the finalizer. It will listen for processes to finalize on the OndemandCh channel.
// It will also periodically check for processes to finalize based on their start date and duration.
// The monitorInterval is the interval at which to check for processes to finalize.
// If monitorInterval is 0, it will not check for processes to finalize.
func (f *finalizer) Start(ctx context.Context, monitorInterval time.Duration) {
	f.ctx, f.cancel = context.WithCancel(ctx)

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			select {
			case pid := <-f.OndemandCh:
				go func(pid *types.ProcessID) {
					if err := f.finalize(pid); err != nil {
						log.Errorw(err, fmt.Sprintf("finalizing process %s", pid.String()))
					}
				}(pid) // Use a goroutine to avoid blocking the channel
			case <-f.ctx.Done():
				return
			}
		}
	}()

	if monitorInterval > 0 {
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			ticker := time.NewTicker(monitorInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					f.finalizeByDate(time.Now())
					f.finalizeEnded()
				case <-f.ctx.Done():
					return
				}
			}
		}()
	}

	log.Infow("finalizer started successfully")
}

// Close gracefully shuts down the finalizer and waits for all goroutines to exit.
// This method should be called before closing the database to avoid panics.
func (f *finalizer) Close() {
	// Use a mutex to ensure thread safety if we were to add one
	if f.cancel == nil {
		return
	}

	// Signal all goroutines to stop
	f.cancel()
	f.cancel = nil

	// Create a channel for draining signals
	done := make(chan struct{})

	// Drain the OndemandCh in a separate goroutine with a timeout
	go func() {
		for {
			select {
			case <-f.OndemandCh:
				// Discard pending items
			case <-time.After(100 * time.Millisecond):
				// If no message received in 100ms, assume channel is drained
				close(done)
				return
			}
		}
	}()

	// Wait for the channel to be drained or timeout after 2 seconds
	select {
	case <-done:
		// Channel drained successfully
	case <-time.After(2 * time.Second):
		log.Warnw("timeout while draining finalizer channel")
	}

	// Wait for all goroutines to exit with a timeout
	waitCh := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		log.Infow("finalizer closed successfully")
	case <-time.After(5 * time.Second):
		log.Warnw("some finalizer goroutines did not exit cleanly")
	}
}

// finalizeByDate finalizes all processes that startdate+duration is before the given date
// and that do not have a result yet.
func (f *finalizer) finalizeByDate(date time.Time) {
	pids, err := f.stg.ListProcessWithEncryptionKeys()
	if err != nil {
		log.Errorw(err, "could not list processes")
		return
	}
	for _, pidBytes := range pids {
		pid := new(types.ProcessID)
		if err := pid.Unmarshal(pidBytes); err != nil {
			log.Errorw(err, "could not unmarshal process ID")
			continue
		}

		process, err := f.stg.Process(pid)
		if err != nil {
			continue
		}

		if !process.IsFinalized && process.StartTime.Add(process.Duration).Before(date) {
			log.Debugw("found proces to finalize by date", "pid", pid.String())
			f.OndemandCh <- pid
		}
	}
}

// finalizeEnded finalizes all processes that have ended and do not have a
// result yet. It retrieves the process IDs from storage, checks if they are
// finalized, and if not, sends them to the OndemandCh channel for processing.
func (f *finalizer) finalizeEnded() {
	pids, err := f.stg.ListEndedProcessWithEncryptionKeys()
	if err != nil {
		log.Errorw(err, "could not list ended processes")
		return
	}
	for _, pidBytes := range pids {
		processID := new(types.ProcessID).SetBytes(pidBytes)
		process, err := f.stg.Process(processID)
		if err != nil {
			log.Errorw(err, "could not retrieve process from storage: "+processID.String())
			continue
		}
		if !process.IsFinalized {
			log.Debugw("found ended process to finalize", "pid", processID.String())
			f.OndemandCh <- processID
		} else {
			log.Debugw("process already finalized, skipping", "pid", processID.String())
		}
	}
}

// finalize finalizes a process by decrypting the accumulators and storing the result.
// It retrieves the process from storage, decrypts the accumulators using the encryption keys,
// and stores the result back to storage.
func (f *finalizer) finalize(pid *types.ProcessID) error {
	f.lock.Lock()
	defer f.lock.Unlock()

	// Retrieve the process from storage
	process, err := f.stg.Process(pid)
	if err != nil {
		return err
	}

	// Check if the process is already finalized
	if process.IsFinalized {
		log.Debugw("process already finalized, skipping", "pid", pid.String())
		return nil
	}
	log.Debugw("finalizing process", "pid", pid.String())

	// Fetch the encryption key
	encryptionPubKey, encryptionPrivKey, err := f.stg.EncryptionKeys(pid)
	if err != nil || encryptionPubKey == nil || encryptionPrivKey == nil {
		return fmt.Errorf("could not retrieve encryption keys for process %s: %w", pid.String(), err)
	}

	// Open the state for the process
	st, err := state.LoadOnRoot(f.stateDB, pid.BigInt(), process.StateRoot.MathBigInt())
	if err != nil {
		return fmt.Errorf("could not open state for process %s: %w", pid.String(), err)
	}

	// Fetch the encrypted accumulators
	encryptedAddAccumulator, ok := st.ResultsAdd()
	if !ok {
		return fmt.Errorf("could not retrieve encrypted add accumulator for process %s", pid.String())
	}
	encryptedSubAccumulator, ok := st.ResultsSub()
	if !ok {
		return fmt.Errorf("could not retrieve encrypted sub accumulator for process %s", pid.String())
	}

	// Decrypt the accumulators
	maxValue := process.BallotMode.MaxValue.MathBigInt().Uint64() * process.Census.MaxVotes.MathBigInt().Uint64()
	if maxValue == 0 {
		maxValue = failbackMaxValue
	}
	startTime := time.Now()
	addAccumulator := [types.FieldsPerBallot]*big.Int{}
	addAccumulatorsEncrypted := [types.FieldsPerBallot]elgamal.Ciphertext{}
	addDecryptionProofs := [types.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedAddAccumulator.Ciphertexts {
		if ct.C1 == nil || ct.C2 == nil {
			return fmt.Errorf("invalid ciphertext for process %s: %v", pid.String(), ct)
		}
		_, result, err := elgamal.Decrypt(encryptionPubKey, encryptionPrivKey, ct.C1, ct.C2, maxValue)
		if err != nil {
			return fmt.Errorf("could not decrypt add accumulator for process %s: %w", pid.String(), err)
		}
		addAccumulator[i] = result
		addAccumulatorsEncrypted[i] = *ct
		addDecryptionProofs[i], err = elgamal.BuildDecryptionProof(encryptionPrivKey, encryptionPubKey, ct.C1, ct.C2, result)
		if err != nil {
			return fmt.Errorf("could not build decryption proof for add accumulator for process %s: %w", pid.String(), err)
		}
	}
	log.Debugw("decrypted add accumulator", "pid", pid.String(), "duration", time.Since(startTime).String(), "result", addAccumulator)

	startTime = time.Now()
	resultsAccumulator := [types.FieldsPerBallot]*big.Int{}
	subAccumulator := [types.FieldsPerBallot]*big.Int{}
	subAccumulatorsEncrypted := [types.FieldsPerBallot]elgamal.Ciphertext{}
	subDecryptionProofs := [types.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedSubAccumulator.Ciphertexts {
		if ct.C1 == nil || ct.C2 == nil {
			return fmt.Errorf("invalid ciphertext for process %s: %v", pid.String(), ct)
		}
		_, result, err := elgamal.Decrypt(encryptionPubKey, encryptionPrivKey, ct.C1, ct.C2, maxValue)
		if err != nil {
			return fmt.Errorf("could not decrypt sub accumulator for process %s: %w", pid.String(), err)
		}
		subAccumulator[i] = result
		subAccumulatorsEncrypted[i] = *ct
		subDecryptionProofs[i], err = elgamal.BuildDecryptionProof(encryptionPrivKey, encryptionPubKey, ct.C1, ct.C2, result)
		if err != nil {
			return fmt.Errorf("could not build decryption proof for sub accumulator for process %s: %w", pid.String(), err)
		}
		resultsAccumulator[i] = new(big.Int).Sub(addAccumulator[i], subAccumulator[i])
	}
	log.Debugw("decrypted sub accumulator", "pid", pid.String(), "duration", time.Since(startTime).String(), "result", subAccumulator)

	// Generate the witness for the circuit
	resultsVerifierWitness, err := results.GenerateWitness(
		st,
		resultsAccumulator,
		addAccumulator,
		subAccumulator,
		addAccumulatorsEncrypted,
		subAccumulatorsEncrypted,
		addDecryptionProofs,
		subDecryptionProofs,
	)
	if err != nil {
		return fmt.Errorf("could not generate witness for process %s: %w", pid.String(), err)
	}
	proof, err := f.prover(
		circuits.ResultsVerifierCurve,
		f.circuits.rvCcs,
		f.circuits.rvPk,
		resultsVerifierWitness,
		solidity.WithProverTargetSolidityVerifier(backend.GROTH16),
	)
	if err != nil {
		return fmt.Errorf("could not generate proof for process %s: %w", pid.String(), err)
	}

	stateRoot, err := st.RootAsBigInt()
	if err != nil {
		return fmt.Errorf("could not get state root for process %s: %w", pid.String(), err)
	}

	// Store the result in the process
	return f.setProcessFinalized(pid, &storage.VerifiedResults{
		ProcessID: pid.Marshal(),
		Proof:     proof.(*groth16_bn254.Proof),
		Inputs: storage.ResultsVerifierProofInputs{
			StateRoot: stateRoot,
			Results:   resultsAccumulator,
		},
	})
}

// setProcessFinalized sets the process as finalized in the storage. If the process is already finalized, it does nothing.
func (f *finalizer) setProcessFinalized(pid *types.ProcessID, res *storage.VerifiedResults) error {
	if res == nil {
		return fmt.Errorf("cannot finalize process %s with nil results", pid.String())
	}

	// Transform the results accumulators to types.BigInt
	results := []*types.BigInt{}
	for _, r := range res.Inputs.Results {
		if r == nil {
			r = new(big.Int).SetInt64(0) // Ensure we don't have nil values
		}
		results = append(results, (*types.BigInt)(r))
	}

	// Update the process atomically to avoid race conditions
	var stateRoot *types.BigInt
	if err := f.stg.UpdateProcess(pid.Marshal(), func(p *types.Process) error {
		// Check if already finalized within the lock
		if p.IsFinalized {
			return nil // Already done
		}

		p.IsFinalized = true
		p.Result = results
		stateRoot = p.StateRoot // Capture for logging
		return nil
	}); err != nil {
		return fmt.Errorf("could not update finalized process %s: %w", pid.String(), err)
	}

	// Push the verified results to storage
	if err := f.stg.PushVerifiedResults(res); err != nil {
		return fmt.Errorf("could not store verified results for process %s: %w", pid.String(), err)
	}

	log.Infow("process finalized",
		"pid", pid.String(),
		"stateRoot", stateRoot.String(),
		"result", results)
	return nil
}

// WaitUntilFinalized waits until the process is finalized. Returns the result of the process.
// It ensures proper timeout handling and provides detailed logging for troubleshooting.
func (f *finalizer) WaitUntilFinalized(ctx context.Context, pid *types.ProcessID) ([]*types.BigInt, error) {
	// Create a timeout context if one wasn't already provided
	var cancel context.CancelFunc
	var timeoutCtx context.Context

	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		// Default timeout of 60 seconds if no deadline is set
		timeoutCtx, cancel = context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
	} else {
		timeoutCtx = ctx
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	log.Debugw("waiting for process to be finalized", "pid", pid.String())

	for {
		select {
		case <-ticker.C:
			process, err := f.stg.Process(pid)
			if err != nil {
				log.Errorw(err, fmt.Sprintf("error retrieving process %s during wait", pid.String()))
				return nil, fmt.Errorf("could not retrieve process %s: %w", pid.String(), err)
			}

			if process.IsFinalized && process.Result != nil {
				return process.Result, nil
			}

		case <-timeoutCtx.Done():
			log.Warnw("timeout waiting for process to be finalized", "pid", pid.String())
			return nil, fmt.Errorf("timeout waiting for process %s to be finalized: %w",
				pid.String(), timeoutCtx.Err())

		case <-f.ctx.Done():
			return nil, fmt.Errorf("finalizer is shutting down while waiting for process %s",
				pid.String())
		}
	}
}
