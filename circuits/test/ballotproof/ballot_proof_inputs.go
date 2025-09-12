package ballotprooftest

import (
	"crypto/rand"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-rapidsnark/prover"
	"github.com/iden3/go-rapidsnark/witness"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
)

//go:embed circom_assets/ballot_proof.wasm
var TestCircomCircuit []byte

//go:embed circom_assets/ballot_proof_pkey.zkey
var TestCircomProvingKey []byte

//go:embed circom_assets/ballot_proof_vkey.json
var TestCircomVerificationKey []byte

// GenECDSAaccountForTest generates a new ECDSA account and returns the private
// key, public key and address.
func GenECDSAaccountForTest() (*ethereum.Signer, error) {
	// generate ecdsa keys and address (privKey and publicKey)
	privKey, err := ethereum.NewSigner()
	if err != nil {
		return nil, err
	}
	return privKey, nil
}

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
func GenBallotFieldsForTest(n, max, min int, unique bool) [types.FieldsPerBallot]*types.BigInt {
	fields := [types.FieldsPerBallot]*types.BigInt{}
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

// GenDeterministicBallotFieldsForTest generates a list of n deterministic fields
// based on the provided seed for consistent testing and caching.
func GenDeterministicBallotFieldsForTest(seed int64, n, max, min int, unique bool) [types.FieldsPerBallot]*types.BigInt {
	fields := [types.FieldsPerBallot]*types.BigInt{}
	for i := range len(fields) {
		fields[i] = types.NewInt(0)
	}

	// Use seed-based deterministic generation
	stored := map[string]bool{}
	for i := range n {
		for attempt := 0; ; attempt++ {
			// Generate deterministic field based on seed, index, and attempt
			fieldSeed := seed + int64(i*1000) + int64(attempt)
			fieldValue := int64(min) + (fieldSeed % int64(max-min))
			field := big.NewInt(fieldValue)

			// if it should be unique and it's already stored, try next attempt
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
	calc, err := witness.NewCircom2WitnessCalculator(TestCircomCircuit, true)
	if err != nil {
		return "", "", fmt.Errorf("instance witness calculator: %w", err)
	}
	// calculate witness
	w, err := calc.CalculateWTNSBin(finalInputs, true)
	if err != nil {
		return "", "", fmt.Errorf("calculate witness: %w", err)
	}
	// generate proof
	return prover.Groth16ProverRaw(TestCircomProvingKey, w)
}

// VoterProofResult struct includes all the public information generated by the
// user after ballot proof generation. It includes the value of the given
// process id and address in the format used inside the circuit.
type VoterProofResult struct {
	ProcessID  *big.Int
	Address    *big.Int
	Ballot     *elgamal.Ballot
	Proof      string
	PubInputs  string
	InputsHash *big.Int
	VoteID     types.HexBytes
}

// BallotProofForTest function return the information after proving a valid
// ballot for the voter address, process id and encryption key provided. It
// generates and encrypts the fields for the ballot and generates a proof of
// a valid vote. It returns a *VoterProofResult and an error if it fails.
func BallotProofForTest(address []byte, processID *types.ProcessID, encryptionKey ecc.Point) (*VoterProofResult, error) {
	now := time.Now()
	// generate random fields
	fields := GenBallotFieldsForTest(circuits.MockNumFields, circuits.MockMaxValue, circuits.MockMinValue, circuits.MockUniqueValues > 0)
	// generate voter k
	k, err := elgamal.RandK()
	if err != nil {
		return nil, err
	}
	// generate ballot proof inputs
	ballotProofInputs := &ballotproof.BallotProofInputs{
		ProcessID:     processID.Marshal(),
		Address:       address,
		EncryptionKey: types.SliceOf(encryptionKey.BigInts(), types.BigIntConverter),
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    circuits.MockBallotModeInternal(),
		Weight:        new(types.BigInt).SetInt(circuits.MockWeight),
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
	return &VoterProofResult{
		ProcessID:  proofInputs.CircomInputs.ProcessID.MathBigInt(),
		Address:    proofInputs.CircomInputs.Address.MathBigInt(),
		Ballot:     proofInputs.Ballot,
		Proof:      circomProof,
		PubInputs:  circomPubInputs,
		InputsHash: proofInputs.BallotInputsHash.MathBigInt(),
		VoteID:     proofInputs.VoteID,
	}, nil
}

// BallotProofForTestDeterministic function returns the information after proving a valid
// ballot using deterministic generation for consistent testing and caching.
func BallotProofForTestDeterministic(address []byte, processID *types.ProcessID, encryptionKey ecc.Point, seed int64) (*VoterProofResult, error) {
	now := time.Now()
	// generate deterministic fields
	fields := GenDeterministicBallotFieldsForTest(seed, circuits.MockNumFields, circuits.MockMaxValue, circuits.MockMinValue, circuits.MockUniqueValues > 0)
	// generate deterministic voter k
	k, err := GenDeterministicKForTest(seed + 1000) // offset seed for k generation
	if err != nil {
		return nil, err
	}
	// generate ballot proof inputs
	ballotProofInputs := &ballotproof.BallotProofInputs{
		ProcessID:     processID.Marshal(),
		Address:       address,
		EncryptionKey: types.SliceOf(encryptionKey.BigInts(), types.BigIntConverter),
		K:             new(types.BigInt).SetBigInt(k),
		BallotMode:    circuits.MockBallotModeInternal(),
		Weight:        new(types.BigInt).SetInt(circuits.MockWeight),
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
	return &VoterProofResult{
		ProcessID:  proofInputs.CircomInputs.ProcessID.MathBigInt(),
		Address:    proofInputs.CircomInputs.Address.MathBigInt(),
		Ballot:     proofInputs.Ballot,
		Proof:      circomProof,
		PubInputs:  circomPubInputs,
		InputsHash: proofInputs.BallotInputsHash.MathBigInt(),
		VoteID:     proofInputs.VoteID,
	}, nil
}
