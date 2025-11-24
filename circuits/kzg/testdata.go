package kzg

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
	goethkzg "github.com/crate-crypto/go-eth-kzg"
)

// kzgVerifyCircuit is the test circuit that verifies a KZG opening proof.
// It uses the exported KZG library functions.
type kzgVerifyCircuit struct {
	CommitmentCompressed [3]frontend.Variable                      `gnark:",public"`
	Z                    emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	Y                    emulated.Element[sw_bls12381.ScalarField] `gnark:",public"`
	ProofCompressed      [3]frontend.Variable
}

// Define implements the circuit logic using the exported KZG library functions.
func (c *kzgVerifyCircuit) Define(api frontend.API) error {
	commitment, err := UnmarshalKZGCommitment(api, c.CommitmentCompressed)
	if err != nil {
		return err
	}

	proof, err := UnmarshalKZGProof(api, c.ProofCompressed)
	if err != nil {
		return err
	}

	return VerifyKZGProof(api, commitment, proof, c.Z, c.Y)
}

// kzgContext holds the KZG context for proof generation
var kzgTestContext *goethkzg.Context

func init() {
	var err error
	kzgTestContext, err = goethkzg.NewContext4096Secure()
	if err != nil {
		panic("failed to initialize KZG context for tests")
	}
}

// TestData contains precomputed valid KZG proof data for testing
type TestData struct {
	// Commitment as 48-byte compressed G1 point (3 × 16-byte limbs)
	CommitmentLimbs [3]*big.Int
	// Proof as 48-byte compressed G1 point (3 × 16-byte limbs)
	ProofLimbs [3]*big.Int
	// Evaluation point Z (BLS12-381 Fr element)
	Z *big.Int
	// Claimed value Y (BLS12-381 Fr element)
	Y *big.Int
}

// generateValidKZGData creates a valid KZG commitment and proof for testing
func generateValidKZGData(seed int64) TestData {
	// Create a simple blob with deterministic data based on seed
	blob := &goethkzg.Blob{}
	for i := range 50 {
		val := big.NewInt(seed + int64(i))
		valBytes := make([]byte, 32)
		val.FillBytes(valBytes)
		copy(blob[i*32:(i+1)*32], valBytes)
	}

	// Generate commitment
	commitment, err := kzgTestContext.BlobToKZGCommitment(blob, 0)
	if err != nil {
		panic(fmt.Sprintf("failed to generate commitment: %v", err))
	}

	// Generate evaluation point Z (use a simple deterministic value)
	z := big.NewInt(seed * 12345)
	zScalar := bigIntToScalar(z)

	// Compute KZG proof
	proof, claim, err := kzgTestContext.ComputeKZGProof(blob, zScalar, 0)
	if err != nil {
		panic(fmt.Sprintf("failed to compute KZG proof: %v", err))
	}

	// Extract Y from claim
	y := new(big.Int).SetBytes(claim[:])

	return TestData{
		CommitmentLimbs: bytesToLimbs(commitment[:]),
		ProofLimbs:      bytesToLimbs(proof[:]),
		Z:               z,
		Y:               y,
	}
}

// ValidTestData1 returns a valid KZG proof test case
func ValidTestData1() TestData {
	return generateValidKZGData(1)
}

// ValidTestData2 returns another valid KZG proof test case with different values
func ValidTestData2() TestData {
	return generateValidKZGData(2)
}

// InvalidTestData returns test data with an invalid proof
func InvalidTestData() TestData {
	// Start with valid data
	validData := generateValidKZGData(999)

	// Corrupt the proof to make it invalid
	validData.ProofLimbs[0] = new(big.Int).Add(validData.ProofLimbs[0], big.NewInt(1))

	return validData
}

// ProgressiveTestData generates test data for progressive complexity tests
func ProgressiveTestData(seed int) TestData {
	return generateValidKZGData(int64(seed))
}

// bytesToLimbs converts 48 bytes to 3 × 16-byte limbs (big-endian)
func bytesToLimbs(b []byte) [3]*big.Int {
	if len(b) != 48 {
		panic("bytesToLimbs requires exactly 48 bytes")
	}

	var limbs [3]*big.Int
	for i := 0; i < 3; i++ {
		// Each limb is 16 bytes
		limbBytes := make([]byte, 32) // Pad to 32 bytes for big.Int
		copy(limbBytes[16:], b[i*16:(i+1)*16])
		limbs[i] = new(big.Int).SetBytes(limbBytes)
	}
	return limbs
}

// ToCircuitWitness converts TestData to circuit witness format
func (td TestData) ToCircuitWitness() kzgVerifyCircuit {
	return kzgVerifyCircuit{
		CommitmentCompressed: [3]frontend.Variable{
			td.CommitmentLimbs[0],
			td.CommitmentLimbs[1],
			td.CommitmentLimbs[2],
		},
		Z: emulated.ValueOf[sw_bls12381.ScalarField](td.Z),
		Y: emulated.ValueOf[sw_bls12381.ScalarField](td.Y),
		ProofCompressed: [3]frontend.Variable{
			td.ProofLimbs[0],
			td.ProofLimbs[1],
			td.ProofLimbs[2],
		},
	}
}

// ToPublicWitness converts TestData to public witness (commitment, Z, Y only)
func (td TestData) ToPublicWitness() kzgVerifyCircuit {
	return kzgVerifyCircuit{
		CommitmentCompressed: [3]frontend.Variable{
			td.CommitmentLimbs[0],
			td.CommitmentLimbs[1],
			td.CommitmentLimbs[2],
		},
		Z: emulated.ValueOf[sw_bls12381.ScalarField](td.Z),
		Y: emulated.ValueOf[sw_bls12381.ScalarField](td.Y),
	}
}

// bigIntToScalar converts a big.Int to a goethkzg.Scalar
func bigIntToScalar(x *big.Int) goethkzg.Scalar {
	var scalar goethkzg.Scalar
	be := make([]byte, 32)
	x.FillBytes(be)
	copy(scalar[:], be[:])
	return scalar
}
