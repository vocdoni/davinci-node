package statetransition

import (
	"encoding/json"
	"fmt"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bw6761"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
)

// WitnessForDebug returns a shallow-copied witness without AggregatorVK
func WitnessForDebug(witness *StateTransitionCircuit) *StateTransitionCircuit {
	if witness == nil {
		return nil
	}
	sanitized := *witness
	sanitized.AggregatorVK = stdgroth16.VerifyingKey[sw_bw6761.G1Affine, sw_bw6761.G2Affine, sw_bw6761.GTEl]{}
	return &sanitized
}

// MarshalWitnessForDebug marshals a sanitized witness as pretty-printed JSON.
func MarshalWitnessForDebug(witness *StateTransitionCircuit) ([]byte, error) {
	sanitized := WitnessForDebug(witness)
	if sanitized == nil {
		return nil, fmt.Errorf("witness is nil")
	}
	out, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal witness: %w", err)
	}
	return out, nil
}
