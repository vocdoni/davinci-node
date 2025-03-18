package ballotprooftest

import (
	"crypto/ecdsa"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"time"

	gecdsa "github.com/consensys/gnark-crypto/ecc/secp256k1/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/iden3/go-iden3-crypto/babyjub"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/iden3/go-iden3-crypto/poseidon"
	"github.com/iden3/go-rapidsnark/prover"
	"github.com/iden3/go-rapidsnark/witness"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/format"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/util"
)

//go:embed circom_assets/ballot_proof.wasm
var TestCircomCircuit []byte

//go:embed circom_assets/ballot_proof_pkey.zkey
var TestCircomProvingKey []byte

//go:embed circom_assets/ballot_proof_vkey.json
var TestCircomVerificationKey []byte

// GenECDSAaccountForTest generates a new ECDSA account and returns the private
// key, public key and address.
func GenECDSAaccountForTest() (*ecdsa.PrivateKey, ecdsa.PublicKey, common.Address, error) {
	// generate ecdsa keys and address (privKey and publicKey)
	privKey, err := ethcrypto.GenerateKey()
	if err != nil {
		return nil, ecdsa.PublicKey{}, common.Address{}, err
	}
	return privKey, privKey.PublicKey, ethcrypto.PubkeyToAddress(privKey.PublicKey), nil
}

// SignECDSAForTest signs the data with the private key provided and returns the R and
// S values of the signature.
func SignECDSAForTest(privKey *ecdsa.PrivateKey, data []byte) (*big.Int, *big.Int, error) {
	sigBin, err := ethcrypto.Sign(data, privKey)
	if err != nil {
		return nil, nil, err
	}
	// truncate the signature to 64 bytes (the first 32 bytes are the R value,
	// the second 32 bytes are the S value)
	sigBin = sigBin[:64]
	if valid := ethcrypto.VerifySignature(ethcrypto.CompressPubkey(&privKey.PublicKey), data, sigBin); !valid {
		return nil, nil, fmt.Errorf("invalid signature")
	}
	var sig gecdsa.Signature
	if _, err := sig.SetBytes(sigBin); err != nil {
		return nil, nil, err
	}
	r, s := new(big.Int), new(big.Int)
	r.SetBytes(sig.R[:])
	s.SetBytes(sig.S[:])
	return r, s, nil
}

// GenEncryptionKeyForTest generates a new encryption key for testing
// purposes. It uses the Iden3 implementation of the BabyJubJub curve to
// simplify the process.
func GenEncryptionKeyForTest() ecc.Point {
	privkey := babyjub.NewRandPrivKey()

	x, y := format.FromTEtoRTE(privkey.Public().X, privkey.Public().Y)
	return new(bjj.BJJ).SetPoint(x, y)
}

// GenBallotFieldsForTest generates a list of n random fields between min and max
// values. If unique is true, the fields will be unique.
// The items between n and NFields are padded with big.Int(0)
func GenBallotFieldsForTest(n, max, min int, unique bool) [circuits.FieldsPerBallot]*big.Int {
	fields := [circuits.FieldsPerBallot]*big.Int{}
	for i := 0; i < len(fields); i++ {
		fields[i] = big.NewInt(0)
	}
	stored := map[string]bool{}
	for i := 0; i < n; i++ {
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
				fields[i] = field
				stored[field.String()] = true
				break
			}
		}
	}
	return fields
}

// GenCommitmentAndNullifierForTest generates a commitment and nullifier for the
// given address, processID and secret values. It uses the Poseidon hash
// function over BabyJubJub curve to generate the commitment and nullifier.
// The commitment is generated using the address, processID and secret value,
// while the nullifier is generated using the commitment and secret value.
func GenCommitmentAndNullifierForTest(address, processID, secret []byte) (*big.Int, *big.Int, error) {
	commitment, err := poseidon.Hash([]*big.Int{
		crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(address)),
		crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(processID)),
		crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(secret)),
	})
	if err != nil {
		return nil, nil, err
	}
	nullifier, err := poseidon.Hash([]*big.Int{
		commitment,
		crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(secret)),
	})
	if err != nil {
		return nil, nil, err
	}
	return commitment, nullifier, nil
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
	Nullifier  *big.Int
	Commitment *big.Int
	Ballot     *elgamal.Ballot
	Proof      string
	PubInputs  string
	InputsHash *big.Int
}

// BallotProofForTest function return the information after proving a valid ballot
// for the voter address, process id and encryption key provided. It generates
// and encrypts the fields for the ballot, the nullifier and the commitment for
// the user and generates a proof of a valid vote. It returns a *VoterProofResult
// and an error if it fails.
func BallotProofForTest(address, processId []byte, encryptionKey ecc.Point) (*VoterProofResult, error) {
	now := time.Now()
	log.Println("BallotProof inputs generation start")
	// generate random fields
	fields := GenBallotFieldsForTest(circuits.MockMaxCount, circuits.MockMaxValue, circuits.MockMinValue, circuits.MockForceUniqueness > 0)
	// encrypt the fields
	k, err := elgamal.RandK()
	if err != nil {
		return nil, err
	}
	ballot, err := elgamal.NewBallot(encryptionKey).Encrypt(fields, encryptionKey, k)
	if err != nil {
		return nil, err
	}
	// get encryption key point
	circomEncryptionKeyX, circomEncryptionKeyY := format.FromRTEtoTE(encryptionKey.Point())

	// generate and store voter nullifier and commitments
	secret := util.RandomBytes(16)
	commitment, nullifier, err := GenCommitmentAndNullifierForTest(address, processId, secret)
	if err != nil {
		return nil, err
	}
	ffAddress := crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(address))
	ffProcessID := crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(processId))
	// group the circom inputs to hash
	bigCircomInputs := []*big.Int{ffProcessID}
	bigCircomInputs = append(bigCircomInputs, circuits.MockBallotMode().Serialize()...)
	bigCircomInputs = append(bigCircomInputs,
		circomEncryptionKeyX,
		circomEncryptionKeyY,
		ffAddress,
		commitment,
		nullifier,
	)
	bigCircomInputs = append(bigCircomInputs, BallotFromRTEtoTE(ballot).BigInts()...)
	bigCircomInputs = append(bigCircomInputs, big.NewInt(int64(circuits.MockWeight)))
	circomInputsHash, err := mimc7.Hash(bigCircomInputs, nil)
	if err != nil {
		return nil, err
	}
	// init circom inputs
	circomInputs := map[string]any{
		"fields":           circuits.BigIntArrayToStringArray(fields[:], circuits.FieldsPerBallot),
		"max_count":        fmt.Sprint(circuits.MockMaxCount),
		"force_uniqueness": fmt.Sprint(circuits.MockForceUniqueness),
		"max_value":        fmt.Sprint(circuits.MockMaxValue),
		"min_value":        fmt.Sprint(circuits.MockMinValue),
		"max_total_cost":   fmt.Sprint(int(math.Pow(float64(circuits.MockMaxValue), float64(circuits.MockCostExp))) * circuits.MockMaxCount),
		"min_total_cost":   fmt.Sprint(circuits.MockMaxCount),
		"cost_exp":         fmt.Sprint(circuits.MockCostExp),
		"cost_from_weight": fmt.Sprint(circuits.MockCostFromWeight),
		"address":          ffAddress.String(),
		"weight":           fmt.Sprint(circuits.MockWeight),
		"process_id":       ffProcessID.String(),
		"pk":               []string{circomEncryptionKeyX.String(), circomEncryptionKeyY.String()},
		"k":                k.String(),
		"cipherfields":     circuits.BigIntArrayToStringArray(BallotFromRTEtoTE(ballot).BigInts(), circuits.FieldsPerBallot*elgamal.BigIntsPerCiphertext),
		"nullifier":        nullifier.String(),
		"commitment":       commitment.String(),
		"secret":           crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), new(big.Int).SetBytes(secret)).String(),
		"inputs_hash":      circomInputsHash.String(),
	}
	bCircomInputs, err := json.Marshal(circomInputs)
	if err != nil {
		return nil, err
	}
	// create circom proof and public signals
	circomProof, circomPubInputs, err := CompileAndGenerateProofForTest(bCircomInputs)
	if err != nil {
		return nil, fmt.Errorf("create circom proof: %w", err)
	}
	log.Printf("Ballot proof generation ends, it tooks %s", time.Since(now))
	return &VoterProofResult{
		ProcessID:  ffProcessID,
		Address:    ffAddress,
		Nullifier:  nullifier,
		Commitment: commitment,
		Ballot:     ballot,
		Proof:      circomProof,
		PubInputs:  circomPubInputs,
		InputsHash: circomInputsHash,
	}, nil
}

func BallotFromRTEtoTE(rteBallot *elgamal.Ballot) *elgamal.Ballot {
	teBallot := elgamal.NewBallot(curves.New(rteBallot.CurveType))
	for i := range rteBallot.Ciphertexts {
		teBallot.Ciphertexts[i].C1 = teBallot.Ciphertexts[i].C1.SetPoint(
			format.FromRTEtoTE(rteBallot.Ciphertexts[i].C1.Point()))
		teBallot.Ciphertexts[i].C2 = teBallot.Ciphertexts[i].C2.SetPoint(
			format.FromRTEtoTE(rteBallot.Ciphertexts[i].C2.Point()))
	}
	return teBallot
}
