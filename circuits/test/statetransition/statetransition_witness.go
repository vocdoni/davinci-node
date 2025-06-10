package statetransitiontest

import (
	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	"github.com/consensys/gnark/std/recursion/groth16"

	"github.com/vocdoni/davinci-node/circuits/statetransition"
)

func CircuitPlaceholder() *statetransition.StateTransitionCircuit {
	proof, vk := DummyAggProofPlaceholder()
	return CircuitPlaceholderWithProof(proof, vk)
}

func CircuitPlaceholderWithProof(
	proof *groth16.Proof[sw_bw6761.G1Affine, sw_bw6761.G2Affine],
	vk *groth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl],
) *statetransition.StateTransitionCircuit {
	return &statetransition.StateTransitionCircuit{
		AggregatorProof: *proof,
		AggregatorVK:    *vk,
	}
}
