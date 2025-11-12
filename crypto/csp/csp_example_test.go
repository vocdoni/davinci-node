package csp

import (
	"math/rand"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func TestExampleCSP(t *testing.T) {
	// Example usage of the CSP interface
	origin := types.CensusOriginCSPEdDSABLS12377V1
	seed := []byte("example_seed")

	// Create a new CSP with the specified origin and seed
	csp, err := New(origin, seed)
	if err != nil {
		t.Fatalf("Error creating CSP: %v", err)
	}
	t.Log("CSP created successfully:", csp)

	// Random Ethereum address for the organization that creates the process
	orgAddress := common.BytesToAddress(util.RandomBytes(20))
	// Mock process ID for the example
	processID := &types.ProcessID{
		Address: orgAddress,
		Version: []byte{0x00, 0x00, 0x00, 0x01}, // Example prefix
		Nonce:   rand.Uint64(),                  // Random nonce for the process
	}
	// Random Ethereum address for the voter
	voterAddress := common.BytesToAddress(util.RandomBytes(20))
	// Generate a census proof for the voter
	proof, err := csp.GenerateProof(processID, voterAddress)
	if err != nil {
		t.Fatalf("Error generating proof: %v", err)
	}
	t.Log("Census proof generated successfully:", proof)

	// Verify the generated proof
	if err := csp.VerifyProof(proof); err != nil {
		t.Fatalf("Error verifying proof: %v", err)
	}
	t.Log("Census proof verified successfully")
}
