package finalizer

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

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

// Finalizer is responsible for finalizing processes.
type Finalizer struct {
	stg        *storage.Storage
	stateDB    db.Database
	OndemandCh chan *types.ProcessID
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// New creates a new Finalizer instance.
func New(stg *storage.Storage, stateDB db.Database) *Finalizer {
	// We'll create the context in Start() now to avoid premature cancellation
	return &Finalizer{
		stg:        stg,
		stateDB:    stateDB,
		OndemandCh: make(chan *types.ProcessID, 10), // Use buffered channel to prevent blocking
	}
}

// Start starts the finalizer. It will listen for processes to finalize on the OndemandCh channel.
// It will also periodically check for processes to finalize based on their start date and duration.
// The monitorInterval is the interval at which to check for processes to finalize.
// If monitorInterval is 0, it will not check for processes to finalize.
func (f *Finalizer) Start(ctx context.Context, monitorInterval time.Duration) {
	f.ctx, f.cancel = context.WithCancel(ctx)

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			select {
			case pid := <-f.OndemandCh:
				if err := f.finalize(pid); err != nil {
					log.Errorw(err, fmt.Sprintf("finalizing process %x", pid.Marshal()))
				}
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
func (f *Finalizer) Close() {
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
func (f *Finalizer) finalizeByDate(date time.Time) {
	pids, err := f.stg.ListProcesses()
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
			log.Errorw(err, "could not retrieve process")
			continue
		}

		if !process.IsFinalized && process.StartTime.Add(process.Duration).Before(date) {
			log.Debugw("found proces to finalize by date", "pid", pid.String())
			f.OndemandCh <- pid
		}
	}
}

// finalize finalizes a process by decrypting the accumulators and storing the result.
// It retrieves the process from storage, decrypts the accumulators using the encryption keys,
// and stores the result back to storage.
func (f *Finalizer) finalize(pid *types.ProcessID) error {
	log.Debugw("finalizing process", "pid", pid.String())
	// Retrieve the process from storage
	process, err := f.stg.Process(pid)
	if err != nil {
		return err
	}

	// Check if the process is already finalized
	if process.IsFinalized {
		return fmt.Errorf("process %x already finalized", pid.Marshal())
	}

	// Fetch the encryption key
	encryptionPubKey, encryptionPrivKey, err := f.stg.EncryptionKeys(pid)
	if err != nil {
		return fmt.Errorf("could not retrieve encryption keys for process %x: %w", pid.Marshal(), err)
	}
	if encryptionPubKey == nil || encryptionPrivKey == nil {
		return fmt.Errorf("encryption keys for process %x are nil", pid.Marshal())
	}

	// Open the state for the process
	st, err := state.New(f.stateDB, pid.BigInt())
	if err != nil {
		return fmt.Errorf("could not open state for process %x: %w", pid.Marshal(), err)
	}

	// Fetch the encrypted accumulators
	encryptedAddAccumulator := st.ResultsAdd()
	if encryptedAddAccumulator == nil {
		return fmt.Errorf("could not retrieve encrypted add accumulator for process %x", pid.Marshal())
	}
	encryptedSubAccumulator := st.ResultsSub()
	if encryptedSubAccumulator == nil {
		return fmt.Errorf("could not retrieve encrypted sub accumulator for process %x", pid.Marshal())
	}

	// Decrypt the accumulators
	maxValue := process.BallotMode.MaxValue.MathBigInt().Uint64() * process.Census.MaxVotes.MathBigInt().Uint64()
	if maxValue == 0 {
		maxValue = failbackMaxValue
	}
	startTime := time.Now()
	addAccumulator := make([]*big.Int, len(encryptedAddAccumulator.Ciphertexts))
	for i, ct := range encryptedAddAccumulator.Ciphertexts {
		if ct.C1 == nil || ct.C2 == nil {
			return fmt.Errorf("invalid ciphertext for process %x: %v", pid.Marshal(), ct)
		}
		_, result, err := elgamal.Decrypt(encryptionPubKey, encryptionPrivKey, ct.C1, ct.C2, maxValue)
		if err != nil {
			return fmt.Errorf("could not decrypt add accumulator for process %x: %w", pid.Marshal(), err)
		}
		addAccumulator[i] = result
	}
	log.Debugw("decrypted add accumulator", "pid", pid.String(), "duration", time.Since(startTime).String(), "result", addAccumulator)

	startTime = time.Now()
	subAccumulator := make([]*big.Int, len(encryptedSubAccumulator.Ciphertexts))
	for i, ct := range encryptedSubAccumulator.Ciphertexts {
		_, result, err := elgamal.Decrypt(encryptionPubKey, encryptionPrivKey, ct.C1, ct.C2, maxValue)
		if err != nil {
			return fmt.Errorf("could not decrypt sub accumulator for process %x: %w", pid.Marshal(), err)
		}
		subAccumulator[i] = result
	}
	log.Debugw("decrypted sub accumulator", "pid", pid.String(), "duration", time.Since(startTime).String(), "result", subAccumulator)

	// Substract the sub accumulator from the add accumulator
	process.Result = make([]*types.BigInt, len(addAccumulator))
	for i := range addAccumulator {
		process.Result[i] = new(types.BigInt).Sub((*types.BigInt)(addAccumulator[i]), (*types.BigInt)(subAccumulator[i]))
	}
	process.IsFinalized = true

	// Store the finalized process back to storage
	if err := f.stg.SetProcess(process); err != nil {
		return fmt.Errorf("could not store finalized process %x: %w", pid.Marshal(), err)
	}

	log.Infow("finalized process", "pid", pid.String(), "result", process.Result)
	return nil
}

// WaitUntilFinalized waits until the process is finalized. Returns the result of the process.
// It ensures proper timeout handling and provides detailed logging for troubleshooting.
func (f *Finalizer) WaitUntilFinalized(ctx context.Context, pid *types.ProcessID) ([]*types.BigInt, error) {
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
				return nil, fmt.Errorf("could not retrieve process %x: %w", pid.Marshal(), err)
			}

			if process.IsFinalized && process.Result != nil {
				log.Infow("process successfully finalized", "pid", pid.String())
				return process.Result, nil
			}

		case <-timeoutCtx.Done():
			log.Warnw("timeout waiting for process to be finalized", "pid", pid.String())
			return nil, fmt.Errorf("timeout waiting for process %x to be finalized: %w",
				pid.Marshal(), timeoutCtx.Err())

		case <-f.ctx.Done():
			return nil, fmt.Errorf("finalizer is shutting down while waiting for process %x",
				pid.Marshal())
		}
	}
}
