package circuitstest

import (
	"context"
	"fmt"
	"sync"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/config"
	"github.com/vocdoni/davinci-node/log"
)

var (
	voteVerifierCCSHashOnce sync.Once
	voteVerifierCCSHash     string
	voteVerifierCCSHashErr  error

	aggregatorCCSHashOnce sync.Once
	aggregatorCCSHash     string
	aggregatorCCSHashErr  error
)

// VoteVerifierCircuitCCSHash returns a deterministic hash of the compiled vote verifier CCS.
func VoteVerifierCircuitCCSHash() (string, error) {
	voteVerifierCCSHashOnce.Do(func() {
		ccs, err := voteverifier.Compile()
		if err != nil {
			voteVerifierCCSHashErr = fmt.Errorf("compile vote verifier circuit definition: %w", err)
			return
		}
		hash, err := circuits.HashConstraintSystem(ccs)
		if err != nil {
			voteVerifierCCSHashErr = fmt.Errorf("hash vote verifier constraint system: %w", err)
			return
		}
		voteVerifierCCSHash = hash
	})
	return voteVerifierCCSHash, voteVerifierCCSHashErr
}

// AggregatorCircuitCCSHash returns a deterministic hash of the compiled aggregator CCS.
func AggregatorCircuitCCSHash() (string, error) {
	aggregatorCCSHashOnce.Do(func() {
		voteVerifierCCS, err := voteverifier.Compile()
		if err != nil {
			aggregatorCCSHashErr = fmt.Errorf("compile vote verifier circuit definition: %w", err)
			return
		}
		voteVerifierCCSHash, err := circuits.HashConstraintSystem(voteVerifierCCS)
		if err != nil {
			aggregatorCCSHashErr = fmt.Errorf("hash vote verifier circuit definition: %w", err)
			return
		}
		voteVerifierVK, err := circuits.LoadVerifyingKeyFromLocalHash(voteverifier.Artifacts.Curve(), config.VoteVerifierVerificationKeyHash)
		if err != nil {
			if voteVerifierCCSHash == config.VoteVerifierCircuitHash {
				err := voteverifier.Artifacts.DownloadVerifyingKey(context.Background())
				if err != nil {
					log.Warnw("downloading voteverifier VK failed", "CCSHash", voteVerifierCCSHash, "error", err)
				} else {
					voteVerifierVK, err = circuits.LoadVerifyingKeyFromLocalHash(voteverifier.Artifacts.Curve(), config.VoteVerifierVerificationKeyHash)
					if err != nil {
						aggregatorCCSHashErr = fmt.Errorf("load vote verifier VK failed: %w", err)
						return
					}
				}
			} else {
				_, voteVerifierVK, err = groth16.Setup(voteVerifierCCS)
				if err != nil {
					aggregatorCCSHashErr = fmt.Errorf("setup vote verifier circuit: %w", err)
					return
				}
			}
		}
		aggregatorCCS, err := aggregator.Compile(voteVerifierCCS, voteVerifierVK)
		if err != nil {
			aggregatorCCSHashErr = fmt.Errorf("compile aggregator circuit definition: %w", err)
			return
		}
		hash, err := circuits.HashConstraintSystem(aggregatorCCS)
		if err != nil {
			aggregatorCCSHashErr = fmt.Errorf("hash aggregator constraint system: %w", err)
			return
		}
		aggregatorCCSHash = hash
	})
	return aggregatorCCSHash, aggregatorCCSHashErr
}
