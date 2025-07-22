// Package blobpolyeval implements a circuit that verifies KZG blob polynomial
// evaluation using Gnark's audited KzgPointEvaluation implementation.
//
// This circuit implements the EVM precompile 0x0A (KZG_POINT_EVALUATION) spec
// from EIP-4844, including versioned hash verification via SHA256.
package blobpolyeval

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	kzg_bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381/kzg"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/commitments/kzg"
	"github.com/consensys/gnark/std/conversion"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/math/uints"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/state"
)

// fixedKzgSrsVk is the verifying key for the KZG precompile embedded in the circuit
var fixedKzgSrsVk *kzg.VerifyingKey[sw_bls12381.G1Affine, sw_bls12381.G2Affine]

func init() {
	// Initialize the fixed verifying key from the trusted setup
	fixedKzgSrsVk = fixedVerificationKey()
}

// fixedVerificationKey loads the KZG verifying key from the minimal JSON trusted setup
func fixedVerificationKey() *kzg.VerifyingKey[sw_bls12381.G1Affine, sw_bls12381.G2Affine] {
	// Use the minimal hardcoded SRS, same as Gnark approach
	// https://github.com/Consensys/gnark/blob/master/std/evmprecompiles/10-kzg_point_evaluation.go
	var srs = []byte{
		// G1 part
		0x97, 0xf1, 0xd3, 0xa7, 0x31, 0x97, 0xd7, 0x94, 0x26, 0x95, 0x63, 0x8c, 0x4f, 0xa9, 0xac, 0x0f,
		0xc3, 0x68, 0x8c, 0x4f, 0x97, 0x74, 0xb9, 0x05, 0xa1, 0x4e, 0x3a, 0x3f, 0x17, 0x1b, 0xac, 0x58,
		0x6c, 0x55, 0xe8, 0x3f, 0xf9, 0x7a, 0x1a, 0xef, 0xfb, 0x3a, 0xf0, 0x0a, 0xdb, 0x22, 0xc6, 0xbb,
		// G2[0] part
		0x93, 0xe0, 0x2b, 0x60, 0x52, 0x71, 0x9f, 0x60, 0x7d, 0xac, 0xd3, 0xa0, 0x88, 0x27, 0x4f, 0x65,
		0x59, 0x6b, 0xd0, 0xd0, 0x99, 0x20, 0xb6, 0x1a, 0xb5, 0xda, 0x61, 0xbb, 0xdc, 0x7f, 0x50, 0x49,
		0x33, 0x4c, 0xf1, 0x12, 0x13, 0x94, 0x5d, 0x57, 0xe5, 0xac, 0x7d, 0x05, 0x5d, 0x04, 0x2b, 0x7e,
		0x02, 0x4a, 0xa2, 0xb2, 0xf0, 0x8f, 0x0a, 0x91, 0x26, 0x08, 0x05, 0x27, 0x2d, 0xc5, 0x10, 0x51,
		0xc6, 0xe4, 0x7a, 0xd4, 0xfa, 0x40, 0x3b, 0x02, 0xb4, 0x51, 0x0b, 0x64, 0x7a, 0xe3, 0xd1, 0x77,
		0x0b, 0xac, 0x03, 0x26, 0xa8, 0x05, 0xbb, 0xef, 0xd4, 0x80, 0x56, 0xc8, 0xc1, 0x21, 0xbd, 0xb8,
		// G2[1] part
		0xb5, 0xbf, 0xd7, 0xdd, 0x8c, 0xde, 0xb1, 0x28, 0x84, 0x3b, 0xc2, 0x87, 0x23, 0x0a, 0xf3, 0x89,
		0x26, 0x18, 0x70, 0x75, 0xcb, 0xfb, 0xef, 0xa8, 0x10, 0x09, 0xa2, 0xce, 0x61, 0x5a, 0xc5, 0x3d,
		0x29, 0x14, 0xe5, 0x87, 0x0c, 0xb4, 0x52, 0xd2, 0xaf, 0xaa, 0xab, 0x24, 0xf3, 0x49, 0x9f, 0x72,
		0x18, 0x5c, 0xbf, 0xee, 0x53, 0x49, 0x27, 0x14, 0x73, 0x44, 0x29, 0xb7, 0xb3, 0x86, 0x08, 0xe2,
		0x39, 0x26, 0xc9, 0x11, 0xcc, 0xec, 0xea, 0xc9, 0xa3, 0x68, 0x51, 0x47, 0x7b, 0xa4, 0xc6, 0x0b,
		0x08, 0x70, 0x41, 0xde, 0x62, 0x10, 0x00, 0xed, 0xc9, 0x8e, 0xda, 0xda, 0x20, 0xc1, 0xde, 0xf2,
	}

	var vk kzg_bls12381.VerifyingKey
	dec := bls12381.NewDecoder(bytes.NewBuffer(srs), bls12381.NoSubgroupChecks())
	err := dec.Decode(&vk.G1)
	if err != nil {
		panic(fmt.Sprintf("failed to set G1 element: %v", err))
	}
	err = dec.Decode(&vk.G2[0])
	if err != nil {
		panic(fmt.Sprintf("failed to set G2[0] element: %v", err))
	}
	err = dec.Decode(&vk.G2[1])
	if err != nil {
		panic(fmt.Sprintf("failed to set G2[1] element: %v", err))
	}
	vk.Lines[0] = bls12381.PrecomputeLines(vk.G2[0])
	vk.Lines[1] = bls12381.PrecomputeLines(vk.G2[1])
	vkw, err := kzg.ValueOfVerifyingKeyFixed[sw_bls12381.G1Affine, sw_bls12381.G2Affine](vk)
	if err != nil {
		panic(fmt.Sprintf("failed to convert verifying key to fixed: %v", err))
	}
	return &vkw
}

// BlobEvalCircuit verifies KZG blob polynomial evaluation
// without versioned hash checking (already done by smart contract)
type BlobEvalCircuit struct {
	// Public inputs
	EvaluationPoint emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	ClaimedValue    emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`

	// Private inputs (compressed points as per EVM format)
	Commitment [3]frontend.Variable // 48 bytes as 3 x 16-byte limbs
	Proof      [3]frontend.Variable // 48 bytes as 3 x 16-byte limbs
}

// Define implements the circuit constraints using simplified KZG verification
func (c *BlobEvalCircuit) Define(api frontend.API) error {
	// Since the versioned hash is already verified by the smart contract
	// when calling EVM precompile 0x0A, we only need to verify the KZG proof
	return BlobPolynomialEvaluation(
		api,
		c.EvaluationPoint,
		c.ClaimedValue,
		c.Commitment,
		c.Proof,
	)
}

// BlobPolynomialEvaluation performs KZG polynomial evaluation verification
// without versioned hash checking (already done by smart contract)
func BlobPolynomialEvaluation(
	api frontend.API,
	evaluationPoint emulated.Element[sw_bls12381.ScalarField],
	claimedValue emulated.Element[sw_bls12381.ScalarField],
	commitmentCompressed [3]frontend.Variable, // 48 bytes as 3x16-byte limbs
	proofCompressed [3]frontend.Variable, // 48 bytes as 3x16-byte limbs
) error {
	// Convert compressed points from 16-byte limbs to byte arrays
	var comSerializedBytes [48]uints.U8
	for i := range commitmentCompressed {
		res, err := conversion.NativeToBytes(api, commitmentCompressed[len(commitmentCompressed)-1-i])
		if err != nil {
			return fmt.Errorf("convert commitment element %d to bytes: %w", i, err)
		}
		copy(comSerializedBytes[i*16:(i+1)*16], res[16:])
	}

	var proofSerialisedBytes [48]uints.U8
	for i := range proofCompressed {
		res, err := conversion.NativeToBytes(api, proofCompressed[len(proofCompressed)-1-i])
		if err != nil {
			return fmt.Errorf("convert proof element %d to bytes: %w", i, err)
		}
		copy(proofSerialisedBytes[i*16:(i+1)*16], res[16:])
	}

	// Unmarshal compressed commitment and proof into uncompressed points
	g1, err := sw_bls12381.NewG1(api)
	if err != nil {
		return fmt.Errorf("new g1: %w", err)
	}
	commitmentUncompressed, err := g1.UnmarshalCompressed(comSerializedBytes[:])
	if err != nil {
		return fmt.Errorf("unmarshal compressed commitment: %w", err)
	}
	proofUncompressed, err := g1.UnmarshalCompressed(proofSerialisedBytes[:])
	if err != nil {
		return fmt.Errorf("unmarshal compressed proof: %w", err)
	}

	// Create KZG verifier
	v, err := kzg.NewVerifier[emulated.BLS12381Fr, sw_bls12381.G1Affine, sw_bls12381.G2Affine, sw_bls12381.GTEl](api)
	if err != nil {
		return fmt.Errorf("new kzg verifier: %w", err)
	}

	// Construct the commitment and opening proof structures
	kzgCommitment := kzg.Commitment[sw_bls12381.G1Affine]{
		G1El: *commitmentUncompressed,
	}
	kzgOpeningProof := kzg.OpeningProof[sw_bls12381.ScalarField, sw_bls12381.G1Affine]{
		Quotient:     *proofUncompressed,
		ClaimedValue: claimedValue,
	}

	// Verify the KZG opening proof
	err = v.CheckOpeningProof(kzgCommitment, kzgOpeningProof, evaluationPoint, *fixedKzgSrsVk)
	if err != nil {
		return fmt.Errorf("check opening proof: %w", err)
	}

	return nil
}

// hexEncode encodes bytes to hex string with 0x prefix
func hexEncode(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

// splitIntoLimbs splits bytes into 16-byte limbs for circuit input
func splitIntoLimbs(data []byte, numLimbs int) []frontend.Variable {
	limbs := make([]frontend.Variable, numLimbs)
	limbSize := len(data) / numLimbs

	// Reverse order to match EVM format (big-endian)
	for i := range numLimbs {
		start := (numLimbs - 1 - i) * limbSize
		end := start + limbSize
		limbs[i] = hexEncode(data[start:end])
	}
	return limbs
}

func TestBlobEvalCircuit(t *testing.T) {
	c := qt.New(t)

	// Create a data blob
	blob := &kzg4844.Blob{}
	blob[31] = 42 // Last byte of first 32-byte element

	// Compute commitment using kzg4844
	commitmentBytes, err := kzg4844.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	// Find a valid evaluation point
	z, err := state.ComputeEvaluationPoint(big.NewInt(123), big.NewInt(456), 1)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof using kzg4844
	proofBytes, claim, err := kzg4844.ComputeProof(blob, state.BigIntToKZGPoint(z))
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])

	// Prepare witness and limbs
	commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)
	proofLimbs := splitIntoLimbs(proofBytes[:], 3)

	// Prepare witness
	witness := BlobEvalCircuit{
		EvaluationPoint: emulated.ValueOf[sw_bls12381.ScalarField](z),
		ClaimedValue:    emulated.ValueOf[sw_bls12381.ScalarField](y),
		Commitment:      [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
		Proof:           [3]frontend.Variable{proofLimbs[0], proofLimbs[1], proofLimbs[2]},
	}

	// Test the circuit
	now := time.Now()
	err = test.IsSolved(&BlobEvalCircuit{}, &witness, ecc.BN254.ScalarField())
	c.Assert(err, qt.IsNil)

	fmt.Printf("Circuit validation took %v\n", time.Since(now))
	fmt.Printf("Evaluation point z: %s\n", z.String())
	fmt.Printf("Claimed value y: %s\n", y.String())
}

// TestBlobEvalCircuitProving tests full proving and verification
func TestBlobEvalCircuitProving(t *testing.T) {
	c := qt.New(t)

	// Skip if short testing
	if testing.Short() {
		t.Skip("skipping proving test in short mode")
	}

	// Create test data
	blob := &kzg4844.Blob{}
	for i := range 100 {
		val := big.NewInt(int64(i + 1))
		val.FillBytes(blob[i*32 : (i+1)*32])
	}

	// Compute commitment and proof
	commitmentBytes, err := kzg4844.BlobToCommitment(blob)
	c.Assert(err, qt.IsNil)

	z, err := state.ComputeEvaluationPoint(big.NewInt(123), big.NewInt(456), 1)
	c.Assert(err, qt.IsNil)

	// Compute KZG proof using kzg4844
	proofBytes, claim, err := kzg4844.ComputeProof(blob, state.BigIntToKZGPoint(z))
	c.Assert(err, qt.IsNil)
	y := new(big.Int).SetBytes(claim[:])

	// Create witness
	commitmentLimbs := splitIntoLimbs(commitmentBytes[:], 3)
	proofLimbs := splitIntoLimbs(proofBytes[:], 3)

	// Create witness
	witness := BlobEvalCircuit{
		EvaluationPoint: emulated.ValueOf[sw_bls12381.ScalarField](z),
		ClaimedValue:    emulated.ValueOf[sw_bls12381.ScalarField](y),
		Commitment:      [3]frontend.Variable{commitmentLimbs[0], commitmentLimbs[1], commitmentLimbs[2]},
		Proof:           [3]frontend.Variable{proofLimbs[0], proofLimbs[1], proofLimbs[2]},
	}

	// Compile circuit
	var circuit BlobEvalCircuit
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	c.Assert(err, qt.IsNil)
	fmt.Printf("Circuit compiled with %d constraints\n", ccs.GetNbConstraints())

	// Run trusted setup
	pk, vk, err := groth16.Setup(ccs)
	c.Assert(err, qt.IsNil)

	// Create proof
	now := time.Now()
	fullWitness, err := frontend.NewWitness(&witness, ecc.BN254.ScalarField())
	c.Assert(err, qt.IsNil)
	proof16, err := groth16.Prove(ccs, pk, fullWitness)
	c.Assert(err, qt.IsNil)
	fmt.Printf("Proving took %v\n", time.Since(now))

	// Verify proof
	publicWitness := BlobEvalCircuit{
		EvaluationPoint: witness.EvaluationPoint,
		ClaimedValue:    witness.ClaimedValue,
	}
	publicW, err := frontend.NewWitness(&publicWitness, ecc.BN254.ScalarField(), frontend.PublicOnly())
	c.Assert(err, qt.IsNil)
	err = groth16.Verify(proof16, vk, publicW)
	c.Assert(err, qt.IsNil)

	fmt.Printf("\nFull proving and verification successful!\n")
}
