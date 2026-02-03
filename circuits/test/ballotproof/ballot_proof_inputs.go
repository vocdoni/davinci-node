package ballotprooftest

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-rapidsnark/prover"
	"github.com/iden3/go-rapidsnark/witness"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

// GenDeterministicECDSAaccountForTest generates a deterministic ECDSA account
// based on the provided index for consistent testing and caching.
func GenDeterministicECDSAaccountForTest(index int) (*ethereum.Signer, error) {
	// Create a deterministic seed based on the index
	seed := make([]byte, 32)
	// Use a simple deterministic pattern: fill with index value
	for i := range seed {
		seed[i] = byte((index + i) % 256)
	}

	// generate deterministic ecdsa keys from seed
	privKey, err := ethereum.NewSignerFromSeed(seed)
	if err != nil {
		return nil, err
	}
	return privKey, nil
}

// SignECDSAForTest signs the data with the private key provided and returns the R and
// S values of the signature.
func SignECDSAForTest(privKey *ethereum.Signer, data []byte) (*ethereum.ECDSASignature, error) {
	return privKey.Sign(data)
}

// GenDeterministicEncryptionKeyForTest generates a deterministic encryption key
// based on the provided seed for consistent testing and caching.
func GenDeterministicEncryptionKeyForTest(seed int64) ecc.Point {
	// Create deterministic seed bytes
	seedBytes := make([]byte, 32)
	binary.BigEndian.PutUint64(seedBytes[24:], uint64(seed))

	// Create a deterministic private key by using the seed as the scalar
	// This is a simple approach that ensures determinism
	privkey := babyjub.PrivateKey(seedBytes)

	x, y := format.FromTEtoRTE(privkey.Public().X, privkey.Public().Y)
	return new(bjj.BJJ).SetPoint(x, y)
}

// GenBallotFieldsForTest generates a list of n random fields between min and max
// values. If unique is true, the fields will be unique.
// The items between n and NFields are padded with big.Int(0)
func GenBallotFieldsForTest(n, max, min int, unique bool) [params.FieldsPerBallot]*types.BigInt {
	fields := [params.FieldsPerBallot]*types.BigInt{}
	for i := range len(fields) {
		fields[i] = types.NewInt(0)
	}
	stored := map[string]bool{}
	for i := range n {
		for {
			// generate random field
			field, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
			if err != nil {
				panic(err)
			}
			field.Add(field, big.NewInt(int64(min)))
			// if it should be unique and it's already stored, skip it,
			// otherwise add it to the list of fields and continue
			if !unique || !stored[field.String()] {
				fields[i] = fields[i].SetBigInt(field)
				stored[field.String()] = true
				break
			}
		}
	}
	return fields
}

// GenDeterministicKForTest generates a deterministic k value for encryption
// based on the provided seed for consistent testing and caching.
func GenDeterministicKForTest(seed int64) (*big.Int, error) {
	// Create a deterministic k value based on the seed
	// Ensure it's within a valid range for elliptic curve operations
	k := big.NewInt(seed)
	if k.Sign() <= 0 {
		k = k.Abs(k)
		if k.Sign() == 0 {
			k = big.NewInt(1)
		}
	}

	// Ensure k is not too large by taking modulo with a reasonable bound
	maxK := big.NewInt(1)
	maxK.Lsh(maxK, 128) // 2^128
	k.Mod(k, maxK)

	// Ensure k is not zero
	if k.Sign() == 0 {
		k = big.NewInt(1)
	}

	return k, nil
}

// proverMu serializes calls to the rapidsnark Groth16 prover, which is not safe for concurrent use
// (CGO/native code can crash or corrupt state when run in parallel).
var proverMu sync.Mutex

// NewBallotWitnessCalculator creates a new witness calculator for the ballot proof circuit.
func NewBallotWitnessCalculator() (*witness.Circom2WitnessCalculator, error) {
	return witness.NewCircom2WitnessCalculator(ballotproof.CircomCircuitWasm, true)
}

// GenerateProofWithCalculator generates a proof using the provided witness calculator.
// This allows reusing the calculator instance (and its WASM runtime) across multiple proofs,
// which improves performance and prevents memory leaks from repeated initialization.
func GenerateProofWithCalculator(calc *witness.Circom2WitnessCalculator, inputs []byte) (string, string, error) {
	finalInputs, err := witness.ParseInputs(inputs)
	if err != nil {
		return "", "", fmt.Errorf("circom inputs: %w", err)
	}
	// calculate witness
	w, err := calc.CalculateWTNSBin(finalInputs, true)
	if err != nil {
		return "", "", fmt.Errorf("calculate witness: %w", err)
	}
	// generate proof (rapidsnark prover is not concurrent-safe)
	proverMu.Lock()
	proof, pubInputs, err := prover.Groth16ProverRaw(ballotproof.CircomProvingKey, w)
	proverMu.Unlock()
	return proof, pubInputs, err
}

// CompileAndGenerateProofForTest compiles a circom circuit, generates the witness and
// generates the proof using the inputs provided. It returns the proof and the
// public signals of the proof. It uses Rapidsnark and Groth16 prover to
// generate the proof.
func CompileAndGenerateProofForTest(inputs []byte) (string, string, error) {
	finalInputs, err := witness.ParseInputs(inputs)
	if err != nil {
		return "", "", fmt.Errorf("circom inputs: %w", err)
	}
	// instance witness calculator
	calc, err := witness.NewCircom2WitnessCalculator(ballotproof.CircomCircuitWasm, true)
	if err != nil {
		return "", "", fmt.Errorf("instance witness calculator: %w", err)
	}
	// calculate witness
	w, err := calc.CalculateWTNSBin(finalInputs, true)
	if err != nil {
		return "", "", fmt.Errorf("calculate witness: %w", err)
	}
	// generate proof (rapidsnark prover is not concurrent-safe)
	proverMu.Lock()
	proof, pubInputs, err := prover.Groth16ProverRaw(ballotproof.CircomProvingKey, w)
	proverMu.Unlock()
	return proof, pubInputs, err
}

// BallotProofResult struct includes all the public information generated by the
// user after ballot proof generation. It includes the value of the given
// process id and address in the format used inside the circuit.
type BallotProofResult struct {
	ProcessID  *big.Int
	Address    *big.Int
	Weight     *big.Int
	Ballot     *elgamal.Ballot
	Proof      string
	PubInputs  string
	InputsHash *big.Int
	VoteID     types.HexBytes
}

// BallotProofForTestDeterministic function returns the information after proving a valid
// ballot using deterministic generation for consistent testing and caching.
func BallotProofForTestDeterministic(address []byte, processID types.ProcessID, encryptionKey ecc.Point, seed int64) (*BallotProofResult, error) {
	now := time.Now()
	// generate deterministic fields
	fields := testutil.GenDeterministicBallotFields(seed)
	// generate deterministic voter k
	k, err := GenDeterministicKForTest(seed + 1000) // offset seed for k generation
	if err != nil {
		return nil, err
	}
	// generate ballot proof inputs
	ballotProofInputs := &ballotproof.BallotProofInputs{
		ProcessID:     processID,
		Address:       address,
		EncryptionKey: types.SliceOf(encryptionKey.BigInts(), types.BigIntConverter),
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    testutil.BallotModeInternal(),
		Weight:        new(types.BigInt).SetInt(testutil.Weight),
		FieldValues:   fields[:],
	}
	proofInputs, err := ballotproof.GenerateBallotProofInputs(ballotProofInputs)
	if err != nil {
		return nil, fmt.Errorf("generate ballot proof inputs: %w", err)
	}
	// generate ballot proof
	bCircomInputs, err := json.Marshal(proofInputs.CircomInputs)
	if err != nil {
		return nil, err
	}
	circomProof, circomPubInputs, err := CompileAndGenerateProofForTest(bCircomInputs)
	if err != nil {
		return nil, fmt.Errorf("create circom proof: %w", err)
	}
	log.Printf("ballot proof generation ends, it tooks %s", time.Since(now))
	return &BallotProofResult{
		ProcessID:  proofInputs.CircomInputs.ProcessID.MathBigInt(),
		Address:    proofInputs.CircomInputs.Address.MathBigInt(),
		Ballot:     proofInputs.Ballot,
		Proof:      circomProof,
		PubInputs:  circomPubInputs,
		InputsHash: proofInputs.BallotInputsHash.MathBigInt(),
		VoteID:     proofInputs.VoteID,
	}, nil
}
