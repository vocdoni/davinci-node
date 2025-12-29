package blobs

import (
	_ "embed"
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bls12381"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/vocdoni/davinci-node/types"
)

// Embedded test data files
//
//go:embed testdata/blobdata1.txt
var blobData1Hex string

//go:embed testdata/blobdata2.txt
var blobData2Hex string

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

// blobEvalCircuitBarycentricOnly tests ONLY barycentric evaluation without KZG verification.
// This circuit does NOT verify the KZG commitment or proof.
type blobEvalCircuitBarycentricOnly struct {
	Z    emulated.Element[FE] `gnark:",public"`
	Y    emulated.Element[FE] `gnark:",public"`
	Blob [N]emulated.Element[FE]
}

func (c *blobEvalCircuitBarycentricOnly) Define(api frontend.API) error {
	std.RegisterHints()
	return VerifyBarycentricEvaluation(api, &c.Z, &c.Y, c.Blob)
}

// blobEvalCircuitBN254 defines the required fields for COMPLETE blob verification
// including barycentric evaluation AND KZG commitment/proof verification.
// This uses native BN254 scalar field variables for the blob data and emulated BLS12-381
// field elements for the evaluation result Y.
// The commitment and proof are provided as limbs for in-circuit z computation.
type blobEvalCircuitBN254 struct {
	ProcessID       frontend.Variable    `gnark:",public"`
	RootHashBefore  frontend.Variable    `gnark:",public"`
	CommitmentLimbs [3]frontend.Variable `gnark:",public"`
	ProofLimbs      [3]frontend.Variable `gnark:",public"`
	Y               emulated.Element[FE] `gnark:",public"` // emulated BLS12-381 Fr
	Blob            [N]frontend.Variable // native BN254 variables
}

func (c *blobEvalCircuitBN254) Define(api frontend.API) error {
	std.RegisterHints()
	return VerifyFullBlobEvaluationBN254(
		api,
		c.ProcessID,
		c.RootHashBefore,
		c.CommitmentLimbs,
		c.ProofLimbs,
		&c.Y,
		c.Blob,
	)
}

// TestData contains precomputed valid KZG proof data for testing
type testData struct {
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
func generateValidKZGData(seed int64) testData {
	// Create a simple blob with deterministic data based on seed
	blob := new(types.Blob)
	for i := range 50 {
		val := big.NewInt(seed + int64(i))
		valBytes := make([]byte, 32)
		val.FillBytes(valBytes)
		copy(blob[i*32:(i+1)*32], valBytes)
	}

	// Generate commitment
	commitment, err := blob.ComputeCommitment()
	if err != nil {
		panic(fmt.Sprintf("failed to generate commitment: %v", err))
	}

	// Generate evaluation point Z (use a simple deterministic value)
	z := big.NewInt(seed * 12345)

	// Compute KZG proof
	proof, claim, err := blob.ComputeProof(z)
	if err != nil {
		panic(fmt.Sprintf("failed to compute KZG proof: %v", err))
	}

	return testData{
		CommitmentLimbs: commitment.ToLimbs(),
		ProofLimbs:      proof.ToLimbs(),
		Z:               z,
		Y:               claim,
	}
}

// ValidTestData1 returns a valid KZG proof test case
func ValidTestData1() testData {
	return generateValidKZGData(1)
}

// ValidTestData2 returns another valid KZG proof test case with different values
func ValidTestData2() testData {
	return generateValidKZGData(2)
}

// InvalidTestData returns test data with an invalid proof
func InvalidTestData() testData {
	// Start with valid data
	validData := generateValidKZGData(999)

	// Corrupt the proof to make it invalid
	validData.ProofLimbs[0] = new(big.Int).Add(validData.ProofLimbs[0], big.NewInt(1))

	return validData
}

// ProgressiveTestData generates test data for progressive complexity tests
func ProgressiveTestData(seed int) testData {
	return generateValidKZGData(int64(seed))
}

// ToCircuitWitness converts testData to circuit witness format
func (td testData) ToCircuitWitness() kzgVerifyCircuit {
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

// ToPublicWitness converts testData to public witness (commitment, Z, Y only)
func (td testData) ToPublicWitness() kzgVerifyCircuit {
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

// hexStrToBlob converts a hex string to a blob
func hexStrToBlob(hexStr string) (*types.Blob, error) {
	blob := new(types.Blob)
	b, err := hexStrToBytes(hexStr)
	if err != nil {
		return nil, err
	}

	if len(blob) != len(b) {
		return nil, fmt.Errorf("blob does not have the correct length, %d", len(b))
	}
	copy(blob[:], b)
	return blob, nil
}

// hexStrToBytes converts a hex string to bytes
func hexStrToBytes(hexStr string) ([]byte, error) {
	// Remove any whitespace/newlines
	cleaned := ""
	for _, c := range hexStr {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			cleaned += string(c)
		}
	}

	result := make([]byte, len(cleaned)/2)
	for i := 0; i < len(result); i++ {
		high := hexCharToNibble(cleaned[i*2])
		low := hexCharToNibble(cleaned[i*2+1])
		result[i] = (high << 4) | low
	}
	return result, nil
}

// hexCharToNibble converts a hex character to its nibble value
func hexCharToNibble(c byte) byte {
	if c >= '0' && c <= '9' {
		return c - '0'
	}
	if c >= 'a' && c <= 'f' {
		return c - 'a' + 10
	}
	if c >= 'A' && c <= 'F' {
		return c - 'A' + 10
	}
	return 0
}

// GetBlobData1 returns the first embedded test blob data
func GetBlobData1() (*types.Blob, error) {
	return hexStrToBlob(blobData1Hex)
}

// GetBlobData2 returns the second embedded test blob data
func GetBlobData2() (*types.Blob, error) {
	return hexStrToBlob(blobData2Hex)
}
