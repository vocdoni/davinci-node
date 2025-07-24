package recursion

import (
	"encoding/json"
	"fmt"
)

// UnmarshalCircomProofJSON parses the JSON-encoded proof data into a SnarkJSProof struct.
func UnmarshalCircomProofJSON(data []byte) (*CircomProof, error) {
	var proof CircomProof
	err := json.Unmarshal(data, &proof)
	if err != nil {
		return nil, fmt.Errorf("failed to parse proof JSON: %v", err)
	}
	return &proof, nil
}

// UnmarshalCircomVerificationKeyJSON parses the JSON-encoded verification key data into a SnarkJSVerificationKey struct.
func UnmarshalCircomVerificationKeyJSON(data []byte) (*CircomVerificationKey, error) {
	var vk CircomVerificationKey
	err := json.Unmarshal(data, &vk)
	if err != nil {
		return nil, fmt.Errorf("failed to parse verification key JSON: %v", err)
	}
	return &vk, nil
}

// UnmarshalCircomPublicSignalsJSON parses the JSON-encoded public signals data into a slice of strings.
func UnmarshalCircomPublicSignalsJSON(data []byte) ([]string, error) {
	// Parse public signals
	var publicSignals []string
	if err := json.Unmarshal(data, &publicSignals); err != nil {
		return nil, fmt.Errorf("error parsing public signals: %w", err)
	}
	return publicSignals, nil
}

// MarshalCircomProofJSON marshals the given CircomProof into pretty‑printed JSON.
func MarshalCircomProofJSON(proof *CircomProof) ([]byte, error) {
	return json.MarshalIndent(proof, "", "  ")
}

// MarshalCircomVerificationKeyJSON marshals the given CircomVerificationKey into pretty‑printed JSON.
func MarshalCircomVerificationKeyJSON(vk *CircomVerificationKey) ([]byte, error) {
	return json.MarshalIndent(vk, "", "  ")
}

// MarshalCircomPublicSignalsJSON marshals the given public signals (slice of strings)
// into pretty‑printed JSON.
func MarshalCircomPublicSignalsJSON(publicSignals []string) ([]byte, error) {
	return json.MarshalIndent(publicSignals, "", "  ")
}
