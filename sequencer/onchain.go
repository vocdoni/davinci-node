package sequencer

import (
	"maps"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
	"github.com/vocdoni/vocdoni-z-sandbox/solidity"
	"github.com/vocdoni/vocdoni-z-sandbox/storage"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func (s *Sequencer) startOnchainProcessor() error {
	const tickInterval = 5 * time.Second
	ticker := time.NewTicker(tickInterval)

	go func() {
		defer ticker.Stop()
		log.Infow("on-chain processor started",
			"tickInterval", tickInterval)

		for {
			select {
			case <-s.ctx.Done():
				log.Infow("on-chain processor stopped")
				return
			case <-ticker.C:
				s.processOnChain()
			}
		}
	}()
	return nil
}

func (s *Sequencer) processOnChain() {
	// copy pids to avoid locking the map for too long
	s.pidsLock.RLock()
	pids := make(map[string]time.Time, len(s.pids))
	maps.Copy(pids, s.pids)
	s.pidsLock.RUnlock()
	// iterate over the process IDs and process the ones that are ready
	for pid := range pids {
		// get a batch ready for uploading on-chain
		batch, batchID, err := s.stg.NextStateTransitionBatch([]byte(pid))
		if err != nil {
			if err != storage.ErrNoMoreElements {
				log.Errorw(err, "failed to get next state transition batch")
			}
			continue
		}
		// convert the gnark proof to a solidity proof
		solidityCommitmentProof := new(solidity.Groth16CommitmentProof)
		if err := solidityCommitmentProof.FromGnarkProof(batch.Proof); err != nil {
			log.Errorw(err, "failed to convert gnark proof to solidity proof")
			continue
		}
		// send the proof to the contract with the public witness
		if err := s.pushToContract(solidityCommitmentProof, batch.PubWitness); err != nil {
			log.Errorw(err, "failed to push to contract")
			continue
		}
		// mark the batch as done
		if err := s.stg.MarkStateTransitionBatchDone(batchID); err != nil {
			log.Errorw(err, "failed to mark state transition batch as done")
			continue
		}
		// update the last update time
		s.pidsLock.Lock()
		s.pids[pid] = time.Now()
		s.pidsLock.Unlock()
	}
}

func (s *Sequencer) pushToContract(proof *solidity.Groth16CommitmentProof, witness types.HexBytes) error {
	return nil
}
