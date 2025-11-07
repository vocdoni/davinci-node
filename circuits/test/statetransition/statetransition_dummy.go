package statetransitiontest

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/prover"
)

// DummyAggCircuit is dummy aggregator circuit
type DummyAggCircuit struct {
	ValidProofs   frontend.Variable                      `gnark:",public"`
	InputsHash    emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	SecretInput   frontend.Variable                      `gnark:",secret"`
	nbConstraints int
}

// Define defines a dummy aggregator circuit
func (c *DummyAggCircuit) Define(api frontend.API) error {
	cmtr, ok := api.(frontend.Committer)
	if !ok {
		return errors.New("api is not a commiter")
	}
	secret, err := cmtr.Commit(c.SecretInput)
	if err != nil {
		return err
	}
	api.AssertIsDifferent(secret, 0)

	res := api.Mul(c.SecretInput, c.SecretInput)
	for i := 2; i < c.nbConstraints; i++ {
		res = api.Mul(res, c.SecretInput)
	}
	api.AssertIsEqual(c.ValidProofs, c.ValidProofs)
	for _, input := range c.InputsHash.Limbs {
		api.AssertIsEqual(input, input)
	}
	return nil
}

// DummyAggPlaceholder function returns the placeholder of a dummy aggregator circuit for
// the constraint.ConstraintSystem provided.
func DummyAggPlaceholder(mainDummyAggCircuit constraint.ConstraintSystem) *DummyAggCircuit {
	return &DummyAggCircuit{nbConstraints: mainDummyAggCircuit.GetNbConstraints()}
}

// DummyAggPlaceholderWithConstraints returns the placeholder of a dummy aggregator circuit
// with the desired number of constraints.
func DummyAggPlaceholderWithConstraints(nbConstraints int) *DummyAggCircuit {
	return &DummyAggCircuit{nbConstraints: nbConstraints}
}

// DummyAggAssignment returns the assignment of a dummy aggregator circuit.
func DummyAggAssignment(validProofs, hash frontend.Variable) *DummyAggCircuit {
	return &DummyAggCircuit{
		ValidProofs: validProofs,
		InputsHash:  emulated.ValueOf[sw_bn254.ScalarField](hash),
		SecretInput: 0,
	}
}

// DummyAggProofPlaceholder returns a placeholder for a dummy aggregator proof
func DummyAggProofPlaceholder() (
	*stdgroth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine],
	*stdgroth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl],
) {
	ccs, _, vk, err := CompileAndSetup(DummyAggPlaceholderWithConstraints(0), circuits.AggregatorCurve.ScalarField())
	if err != nil {
		panic(err)
	}
	// parse dummy proof and witness
	dummyProof := stdgroth16.PlaceholderProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](ccs)
	// set fixed dummy vk in the placeholders
	dummyVK, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](vk)
	if err != nil {
		panic(err)
	}
	return &dummyProof, &dummyVK
}

// DummyAggProof returns a dummy aggregator proof
func DummyAggProof(validProofs, hash frontend.Variable) (
	*stdgroth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine],
	*stdgroth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl], error,
) {
	_, _, proof, vk, err := Prove(
		DummyAggPlaceholderWithConstraints(0), DummyAggAssignment(validProofs, hash),
		circuits.StateTransitionCurve.ScalarField(), circuits.AggregatorCurve.ScalarField())
	if err != nil {
		return nil, nil, err
	}
	// parse dummy proof and witness
	dummyProof, err := stdgroth16.ValueOfProof[sw_bw6761.G1Affine, sw_bw6761.G2Affine](proof)
	if err != nil {
		return nil, nil, fmt.Errorf("dummy proof value error: %w", err)
	}
	// set fixed dummy vk in the placeholders
	dummyVK, err := stdgroth16.ValueOfVerifyingKeyFixed[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl](vk)
	if err != nil {
		return nil, nil, fmt.Errorf("dummy vk value error: %w", err)
	}
	return &dummyProof, &dummyVK, nil
}

func Prove(placeholder, assignment frontend.Circuit, outer *big.Int, field *big.Int) (constraint.ConstraintSystem, witness.Witness, groth16.Proof, groth16.VerifyingKey, error) {
	ccs, pk, vk, err := CompileAndSetup(placeholder, field)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("init error: %w", err)
	}

	// Create the full witness (needed for return value and verification)
	fullWitness, err := frontend.NewWitness(assignment, field)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("full witness error: %w", err)
	}

	// Determine curve from field - AggregatorCurve uses BW6-761
	curve := circuits.AggregatorCurve

	// Generate proof (automatically uses GPU if enabled)
	proof, err := prover.ProveWithWitness(curve, ccs, pk, fullWitness, stdgroth16.GetNativeProverOptions(outer, field))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("proof error: %w", err)
	}
	publicWitness, err := fullWitness.Public()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("pub witness error: %w", err)
	}
	if err = groth16.Verify(proof, vk, publicWitness, stdgroth16.GetNativeVerifierOptions(outer, field)); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("verify error: %w", err)
	}
	return ccs, publicWitness, proof, vk, nil
}

func CompileAndSetup(placeholder frontend.Circuit, field *big.Int) (constraint.ConstraintSystem, groth16.ProvingKey, groth16.VerifyingKey, error) {
	ccs, err := frontend.Compile(field, r1cs.NewBuilder, placeholder)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("compile error: %w", err)
	}

	pk, vk, err := prover.Setup(ccs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("setup error: %w", err)
	}
	return ccs, pk, vk, nil
}
