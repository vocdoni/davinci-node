package circuitstest

import (
	"encoding/hex"
	"fmt"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/aggregator"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/log"
)

// LoadVoteVerifierRuntimeArtifacts returns vote verifier artifacts from the configured
// artifact store when they match the current circuit definition, or rebuilds them in memory.
func LoadVoteVerifierRuntimeArtifacts() (constraint.ConstraintSystem, groth16.ProvingKey, groth16.VerifyingKey, error) {
	ccs, err := voteverifier.Compile()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("compile voteverifier circuit: %w", err)
	}
	return loadOrSetupArtifacts(ccs, voteverifier.Artifacts)
}

// LoadAggregatorRuntimeArtifacts returns aggregator artifacts from the configured
// artifact store when they match the current circuit definition, or rebuilds them in memory.
func LoadAggregatorRuntimeArtifacts() (constraint.ConstraintSystem, groth16.ProvingKey, groth16.VerifyingKey, error) {
	vvCCS, _, vvVK, err := LoadVoteVerifierRuntimeArtifacts()
	if err != nil {
		return nil, nil, nil, err
	}

	ccs, err := aggregator.Compile(vvCCS, vvVK)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("compile aggregator circuit: %w", err)
	}
	return loadOrSetupArtifacts(ccs, aggregator.Artifacts)
}

func loadOrSetupArtifacts(
	ccs constraint.ConstraintSystem,
	artifacts *circuits.CircuitArtifacts,
) (constraint.ConstraintSystem, groth16.ProvingKey, groth16.VerifyingKey, error) {
	name := artifacts.Name()
	currentHash, err := circuits.HashConstraintSystem(ccs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("check %s circuit hash: %w", name, err)
	}
	matches := currentHash == hex.EncodeToString(artifacts.CircuitHash())
	if matches {
		if err := artifacts.LoadAll(); err == nil {
			loadedCCS, err := artifacts.CircuitDefinition()
			if err != nil {
				return nil, nil, nil, fmt.Errorf("load %s circuit definition: %w", name, err)
			}
			loadedPK, err := artifacts.ProvingKey()
			if err != nil {
				return nil, nil, nil, fmt.Errorf("load %s proving key: %w", name, err)
			}
			loadedVK, err := artifacts.VerifyingKey()
			if err != nil {
				return nil, nil, nil, fmt.Errorf("load %s verifying key: %w", name, err)
			}
			return loadedCCS, loadedPK, loadedVK, nil
		} else {
			log.Warnw("configured circuit artifacts unavailable; running setup in memory",
				"circuit", name, "error", err)
		}
	} else {
		log.Warnw("configured circuit artifacts are stale; running setup in memory",
			"circuit", name,
			"configuredHash", hex.EncodeToString(artifacts.CircuitHash()),
			"currentHash", currentHash,
		)
	}

	pk, vk, err := artifacts.Setup(ccs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("setup %s circuit: %w", name, err)
	}
	return ccs, pk, vk, nil
}
