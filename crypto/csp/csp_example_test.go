package csp

import (
	"testing"

	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
)

func TestExampleCSP(t *testing.T) {
	// Example usage of the CSP interface
	origin := types.CensusOriginCSPEdDSABN254V1
	seed := []byte("example_seed")

	// Create a new CSP with the specified origin and seed
	csp, err := New(origin, seed)
	if err != nil {
		t.Fatalf("Error creating CSP: %v", err)
	}
	t.Log("CSP created successfully:", csp)

	// Generate a census proof for the voter
	proof, err := csp.GenerateProof(testutil.RandomProcessID(), testutil.RandomAddress(), types.NewInt(testutil.Weight))
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

func TestCensusRootLenght(t *testing.T) {
	origin := types.CensusOriginCSPEdDSABN254V1

	for range 10000 {
		csp, err := New(origin, nil)
		if err != nil {
			t.Fatalf("Error creating CSP: %v", err)
		}
		root := csp.CensusRoot().Root
		if len(root) != types.CensusRootLength {
			t.Errorf("Census root length is not 32 bytes: %d", len(root))
		}
	}
}
