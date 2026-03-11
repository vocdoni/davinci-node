package blobs

import (
	"fmt"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/log"
)

const falseStr = "false"

// TestKZGVerifyBasic tests basic KZG verification with valid proof data.
func TestKZGVerifyBasic(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}

	testData := ValidTestData1()
	witness := testData.ToCircuitWitness()

	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&kzgVerifyCircuit{}, &witness,
		test.WithCurves(ecc.BN254), test.WithBackends(backend.GROTH16))

	log.Infow("basic KZG verification test passed")
}

// TestKZGVerifyMultipleCases tests KZG verification with different valid test cases.
func TestKZGVerifyMultipleCases(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}

	testCases := []struct {
		name string
		data testData
	}{
		{"ValidTestData1", ValidTestData1()},
		{"ValidTestData2", ValidTestData2()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			witness := tc.data.ToCircuitWitness()

			assert := test.NewAssert(t)
			assert.SolvingSucceeded(&kzgVerifyCircuit{}, &witness,
				test.WithCurves(ecc.BN254), test.WithBackends(backend.GROTH16))

			log.Infow("KZG verification test case passed", "case", tc.name)
		})
	}
}

// TestKZGVerifyProgressive tests the circuit with increasing complexity.
func TestKZGVerifyProgressive(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}

	testSeeds := []int{10, 100, 1000}

	for _, seed := range testSeeds {
		t.Run(fmt.Sprintf("Seed_%d", seed), func(t *testing.T) {
			testData := ProgressiveTestData(seed)
			witness := testData.ToCircuitWitness()

			assert := test.NewAssert(t)
			assert.SolvingSucceeded(&kzgVerifyCircuit{}, &witness,
				test.WithCurves(ecc.BN254), test.WithBackends(backend.GROTH16))

			log.Infow("KZG progressive test passed", "seed", seed)
		})
	}
}

// TestKZGVerifyInvalid tests that the circuit rejects invalid proofs.
func TestKZGVerifyInvalid(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}

	testData := InvalidTestData()
	witness := testData.ToCircuitWitness()

	var circuit kzgVerifyCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	if err != nil {
		t.Fatalf("Failed to compile circuit: %v", err)
	}

	fullWitness, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
	if err != nil {
		t.Fatalf("Failed to create witness: %v", err)
	}

	_, err = ccs.Solve(fullWitness)
	if err == nil {
		t.Fatal("Expected circuit to reject invalid proof, but it was accepted")
	}

	log.Infow("invalid proof correctly rejected")
}

// TestKZGVerifyFullProving performs full circuit compilation and proving.
func TestKZGVerifyFullProving(t *testing.T) {
	c := qt.New(t)

	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}

	testData := ValidTestData1()
	witness := testData.ToCircuitWitness()

	var circuit kzgVerifyCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	c.Assert(err, qt.IsNil)
	log.Infow("circuit compiled", "constraints", ccs.GetNbConstraints())

	log.Infow("running trusted setup")
	pk, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)
	log.Infow("trusted setup completed")

	fullWitness, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
	c.Assert(err, qt.IsNil)

	log.Infow("generating proof")
	proof, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)
	log.Infow("proof generated")

	publicWitness := testData.ToPublicWitness()
	publicW, err := frontend.NewWitness(&publicWitness, ecc.BN254.ScalarField(), frontend.PublicOnly())
	c.Assert(err, qt.IsNil)

	log.Infow("verifying proof")
	err = groth16.Verify(proof, vk, publicW)
	c.Assert(err, qt.IsNil)
	log.Infow("proof verified successfully")
}

// TestKZGVerifyConstraintCount compiles the circuit and reports constraint count.
func TestKZGVerifyConstraintCount(t *testing.T) {
	c := qt.New(t)

	log.Infow("KZG verify circuit constraint analysis")

	var circuit kzgVerifyCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	c.Assert(err, qt.IsNil)

	constraintCount := ccs.GetNbConstraints()
	log.Infof("KZG Verify Circuit Statistics:")
	log.Infof("  Total Constraints: %d", constraintCount)
	log.Infof("  Curve: BN254")
	log.Infof("  Backend: Groth16")
	log.Infof("  Public Inputs: 5 (3 commitment limbs + Z + Y)")
	log.Infof("  Private Inputs: 3 (3 proof limbs)")
}

// BenchmarkKZGVerifyCircuit benchmarks the circuit solving performance.
func BenchmarkKZGVerifyCircuit(b *testing.B) {
	testData := ValidTestData1()
	witness := testData.ToCircuitWitness()

	var circuit kzgVerifyCircuit
	_, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
		if err != nil {
			b.Fatal(err)
		}
	}
}
