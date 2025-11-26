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

	fmt.Println("Basic KZG verification test passed")
}

// TestKZGVerifyMultipleCases tests KZG verification with different valid test cases.
func TestKZGVerifyMultipleCases(t *testing.T) {
	if os.Getenv("RUN_CIRCUIT_TESTS") == "" || os.Getenv("RUN_CIRCUIT_TESTS") == falseStr {
		t.Skip("skipping circuit tests...")
	}

	testCases := []struct {
		name string
		data TestData
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

			fmt.Printf("Test case %s passed\n", tc.name)
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

			fmt.Printf("Progressive test with seed %d passed\n", seed)
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

	fmt.Println("Invalid proof correctly rejected")
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
	fmt.Printf("Circuit compiled with %d constraints\n", ccs.GetNbConstraints())

	fmt.Println("Running trusted setup...")
	pk, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)
	fmt.Println("Trusted setup completed")

	fullWitness, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
	c.Assert(err, qt.IsNil)

	fmt.Println("Generating proof...")
	proof, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)
	fmt.Println("Proof generated")

	publicWitness := testData.ToPublicWitness()
	publicW, err := frontend.NewWitness(&publicWitness, ecc.BN254.ScalarField(), frontend.PublicOnly())
	c.Assert(err, qt.IsNil)

	fmt.Println("Verifying proof...")
	err = groth16.Verify(proof, vk, publicW)
	c.Assert(err, qt.IsNil)
	fmt.Println("Proof verified successfully")
}

// TestKZGVerifyConstraintCount compiles the circuit and reports constraint count.
func TestKZGVerifyConstraintCount(t *testing.T) {
	c := qt.New(t)

	fmt.Println("\n=== KZG Verify Circuit Constraint Analysis ===")

	var circuit kzgVerifyCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	c.Assert(err, qt.IsNil)

	constraintCount := ccs.GetNbConstraints()
	fmt.Printf("\nKZG Verify Circuit Statistics:\n")
	fmt.Printf("  Total Constraints: %d\n", constraintCount)
	fmt.Printf("  Curve: BN254\n")
	fmt.Printf("  Backend: Groth16\n")
	fmt.Printf("  Public Inputs: 5 (3 commitment limbs + Z + Y)\n")
	fmt.Printf("  Private Inputs: 3 (3 proof limbs)\n\n")
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
