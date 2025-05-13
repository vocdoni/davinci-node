package finalizer

import (
	"context"
	"fmt"
	"math/big"
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

type Finalizer struct {
	stg        *storage.Storage
	stateDB    db.Database
	OndemandCh chan *types.ProcessID
}

func New(stg *storage.Storage, stateDB db.Database) *Finalizer {
	return &Finalizer{
		stg:        stg,
		stateDB:    stateDB,
		OndemandCh: make(chan *types.ProcessID),
	}
}

// Start starts the finalizer. It will listen for processes to finalize on the OndemandCh channel.
// It will also periodically check for processes to finalize based on their start date and duration.
// The monitorInterval is the interval at which to check for processes to finalize.
// If monitorInterval is 0, it will not check for processes to finalize.
func (f *Finalizer) Start(ctx context.Context, monitorInterval time.Duration) {
	go func() {
		for {
			select {
			case pid := <-f.OndemandCh:
				if err := f.finalize(pid); err != nil {
					log.Errorw(err, fmt.Sprintf("finalizing process %x", pid.Marshal()))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if monitorInterval > 0 {
		go func() {
			ticker := time.NewTicker(monitorInterval)
			for {
				select {
				case <-ticker.C:
					f.finalizeByDate(time.Now())
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

// finalizeByDate finalizes all processes that startdate+duration is after the given date
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

		if process.Result == nil && process.StartTime.Add(process.Duration).After(date) {
			log.Debugw("found proces to finalize by date", "pid", pid.String())
			f.OndemandCh <- pid
		}
	}
}

func (f *Finalizer) finalize(pid *types.ProcessID) error {
	log.Debugw("finalizing process", "pid", pid.String())
	// Retrieve the process from storage
	process, err := f.stg.Process(pid)
	if err != nil {
		return err
	}

	// Check if the process is already finalized (have results)
	if process.Result != nil {
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

	// Store the finalized process back to storage
	if err := f.stg.SetProcess(process); err != nil {
		return fmt.Errorf("could not store finalized process %x: %w", pid.Marshal(), err)
	}

	log.Infow("finalized process", "pid", pid.String(), "result", process.Result)
	return nil
}

func (f *Finalizer) WaitUntilFinalized(pid *types.ProcessID) error {
	// Check if the process is already finalized
	process, err := f.stg.Process(pid)
	if err != nil {
		return fmt.Errorf("could not retrieve process %x: %w", pid.Marshal(), err)
	}
	if process.Result != nil {
		return nil
	}

	// Wait for the process to be finalized
	for {
		process, err = f.stg.Process(pid)
		if err != nil {
			return fmt.Errorf("could not retrieve process %x: %w", pid.Marshal(), err)
		}
		if process.Result != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	return nil
}
