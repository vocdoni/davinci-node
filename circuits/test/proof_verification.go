package circuitstest

import (
	"fmt"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/prover"
)

// VerifyProofWithWitness derives the public witness from a full witness and verifies the proof.
func VerifyProofWithWitness(proof groth16.Proof, vk groth16.VerifyingKey, fullWitness witness.Witness, opts ...backend.VerifierOption) error {
	startTime := time.Now()
	publicWitness, err := fullWitness.Public()
	if err != nil {
		return fmt.Errorf("create public witness: %w", err)
	}
	if err := groth16.Verify(proof, vk, publicWitness, opts...); err != nil {
		return fmt.Errorf("verify proof: %w", err)
	}
	log.DebugTime("proof verified", startTime, "curve", vk.CurveID().String())
	return nil
}

// ProveAndVerifyWithWitness generates a proof from a full witness and verifies it immediately.
func ProveAndVerifyWithWitness(
	curve ecc.ID,
	ccs constraint.ConstraintSystem,
	pk groth16.ProvingKey,
	vk groth16.VerifyingKey,
	fullWitness witness.Witness,
	proverOpts []backend.ProverOption,
	verifierOpts []backend.VerifierOption,
) (groth16.Proof, error) {
	startTime := time.Now()
	proof, err := prover.ProveWithWitness(curve, ccs, pk, fullWitness, proverOpts...)
	if err != nil {
		return nil, fmt.Errorf("prove witness: %w", err)
	}
	log.DebugTime("proof generated", startTime, "curve", curve.String())
	if err := VerifyProofWithWitness(proof, vk, fullWitness, verifierOpts...); err != nil {
		return nil, err
	}
	return proof, nil
}
