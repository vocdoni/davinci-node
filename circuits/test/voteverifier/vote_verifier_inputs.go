package voteverifiertest

import (
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/arbo"
	primitivestest "github.com/vocdoni/gnark-crypto-primitives/testutil"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"go.vocdoni.io/dvote/util"
)

// VoteVerifierTestResults struct includes relevant data after VerifyVoteCircuit
// inputs generation
type VoteVerifierTestResults struct {
	InputsHashes     []*big.Int
	EncryptionPubKey circuits.EncryptionKey[*big.Int]
	Addresses        []*big.Int
	ProcessID        *big.Int
	CensusRoot       *big.Int
	Nullifiers       []*big.Int
	Commitments      []*big.Int
	Ballots          []elgamal.Ballot
}

// VoterTestData struct includes the information required to generate the test
// inputs for the VerifyVoteCircuit.
type VoterTestData struct {
	PrivKey *ecdsa.PrivateKey
	PubKey  ecdsa.PublicKey
	Address common.Address
}

// VoteVerifierInputsForTest returns the VoteVerifierTestResults, the placeholder
// and the assignments for a VerifyVoteCircuit including the provided voters. If
// processId is nil, it will be randomly generated. If something fails it
// returns an error.
func VoteVerifierInputsForTest(votersData []VoterTestData, processId []byte) (
	VoteVerifierTestResults, voteverifier.VerifyVoteCircuit,
	[]voteverifier.VerifyVoteCircuit, error,
) {
	now := time.Now()
	log.Println("VoteVerifier inputs generation start")
	circomPlaceholder, err := circuits.Circom2GnarkPlaceholder(ballottest.TestCircomVerificationKey)
	if err != nil {
		return VoteVerifierTestResults{}, voteverifier.VerifyVoteCircuit{}, nil, err
	}
	bAddresses, bWeights := [][]byte{}, [][]byte{}
	for _, voter := range votersData {
		bAddresses = append(bAddresses, voter.Address.Bytes())
		bWeights = append(bWeights, new(big.Int).SetInt64(int64(circuits.MockWeight)).Bytes())
	}
	// generate a test census
	testCensus, err := primitivestest.GenerateCensusProofForTest(primitivestest.CensusTestConfig{
		Dir:           fmt.Sprintf("../assets/census%d", util.RandomInt(0, 1000)),
		ValidSiblings: 10,
		TotalSiblings: circuits.CensusProofMaxLevels,
		KeyLen:        20,
		Hash:          arbo.HashFunctionMiMC_BLS12_377,
		BaseField:     arbo.BLS12377BaseField,
	}, bAddresses, bWeights)
	if err != nil {
		return VoteVerifierTestResults{}, voteverifier.VerifyVoteCircuit{}, nil, err
	}
	// common data
	if processId != nil {
		processId = util.RandomBytes(20)
	}
	ek := ballottest.GenEncryptionKeyForTest()
	encryptionKey := circuits.EncryptionKeyFromECCPoint(ek)
	// circuits assignments, voters data and proofs
	var assignments []voteverifier.VerifyVoteCircuit
	inputsHashes, addresses, nullifiers, commitments := []*big.Int{}, []*big.Int{}, []*big.Int{}, []*big.Int{}
	ballots := []elgamal.Ballot{}
	var finalProcessID *big.Int
	for i, voter := range votersData {
		voterProof, err := ballottest.BallotProofForTest(voter.Address.Bytes(), processId, ek)
		if err != nil {
			return VoteVerifierTestResults{}, voteverifier.VerifyVoteCircuit{}, nil, fmt.Errorf("ballotproof inputs: %w", err)
		}
		if finalProcessID == nil {
			finalProcessID = voterProof.ProcessID
		}
		addresses = append(addresses, voterProof.Address)
		commitments = append(commitments, voterProof.Commitment)
		nullifiers = append(nullifiers, voterProof.Nullifier)
		ballots = append(ballots, *voterProof.Ballot)
		// convert the circom inputs hash to the field of the curve used by the
		// circuit as input for MIMC hash
		blsCircomInputsHash := crypto.SignatureHash(voterProof.InputsHash, circuits.VoteVerifierCurve.ScalarField())
		// sign the inputs hash with the private key
		rSign, sSign, err := ballottest.SignECDSAForTest(voter.PrivKey, blsCircomInputsHash)
		if err != nil {
			return VoteVerifierTestResults{}, voteverifier.VerifyVoteCircuit{}, nil, err
		}
		// transform siblings to gnark frontend.Variable
		emulatedSiblings := [circuits.CensusProofMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
		for j, s := range testCensus.Proofs[i].Siblings {
			emulatedSiblings[j] = emulated.ValueOf[sw_bn254.ScalarField](s)
		}
		// hash the inputs of gnark circuit (except weight and including census root)
		// TODO: move this into a helper func, consistent with circuits.VoteVerifierInputs
		hashInputs := []*big.Int{}
		hashInputs = append(hashInputs, voterProof.ProcessID)
		hashInputs = append(hashInputs, testCensus.Root)
		hashInputs = append(hashInputs, circuits.MockBallotMode().Serialize()...)
		hashInputs = append(hashInputs, encryptionKey.Serialize()...)
		hashInputs = append(hashInputs, voterProof.Address)
		hashInputs = append(hashInputs, voterProof.Commitment)
		hashInputs = append(hashInputs, voterProof.Nullifier)
		hashInputs = append(hashInputs, voterProof.Ballot.BigInts()...)
		// hash the inputs to generate the inputs hash
		inputsHash, err := mimc7.Hash(hashInputs, nil)
		if err != nil {
			return VoteVerifierTestResults{}, voteverifier.VerifyVoteCircuit{}, nil, err
		}
		inputsHashes = append(inputsHashes, inputsHash)
		// compose circuit placeholders
		recursiveProof, err := circuits.Circom2GnarkProofForRecursion(ballottest.TestCircomVerificationKey, voterProof.Proof, voterProof.PubInputs)
		if err != nil {
			return VoteVerifierTestResults{}, voteverifier.VerifyVoteCircuit{}, nil, err
		}
		assignments = append(assignments, voteverifier.VerifyVoteCircuit{
			IsValid:    1,
			InputsHash: emulated.ValueOf[sw_bn254.ScalarField](inputsHash),
			// circom inputs
			Vote: circuits.EmulatedVote[sw_bn254.ScalarField]{
				Address:    emulated.ValueOf[sw_bn254.ScalarField](voterProof.Address),
				Commitment: emulated.ValueOf[sw_bn254.ScalarField](voterProof.Commitment),
				Ballot:     *voterProof.Ballot.ToGnarkEmulatedBN254(),
				Nullifier:  emulated.ValueOf[sw_bn254.ScalarField](voterProof.Nullifier),
			},
			UserWeight: emulated.ValueOf[sw_bn254.ScalarField](circuits.MockWeight),
			Process: circuits.Process[emulated.Element[sw_bn254.ScalarField]]{
				ID:            emulated.ValueOf[sw_bn254.ScalarField](voterProof.ProcessID),
				CensusRoot:    emulated.ValueOf[sw_bn254.ScalarField](testCensus.Root),
				EncryptionKey: encryptionKey.BigIntsToEmulatedElementBN254(),
				BallotMode:    circuits.MockBallotModeEmulated(),
			},
			CensusSiblings: emulatedSiblings,
			// signature
			Msg: emulated.ValueOf[emulated.Secp256k1Fr](blsCircomInputsHash),
			PublicKey: gnarkecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]{
				X: emulated.ValueOf[emulated.Secp256k1Fp](voter.PubKey.X),
				Y: emulated.ValueOf[emulated.Secp256k1Fp](voter.PubKey.Y),
			},
			Signature: gnarkecdsa.Signature[emulated.Secp256k1Fr]{
				R: emulated.ValueOf[emulated.Secp256k1Fr](rSign),
				S: emulated.ValueOf[emulated.Secp256k1Fr](sSign),
			},
			// circom proof
			CircomProof: recursiveProof.Proof,
		})
	}
	log.Printf("VoteVerifier inputs generation ends, it tooks %s\n", time.Since(now))
	return VoteVerifierTestResults{
			InputsHashes:     inputsHashes,
			EncryptionPubKey: encryptionKey,
			Addresses:        addresses,
			ProcessID:        finalProcessID,
			CensusRoot:       testCensus.Root,
			Nullifiers:       nullifiers,
			Commitments:      commitments,
			Ballots:          ballots,
		}, voteverifier.VerifyVoteCircuit{
			CircomProof:           circomPlaceholder.Proof,
			CircomVerificationKey: circomPlaceholder.Vk,
		}, assignments, nil
}
