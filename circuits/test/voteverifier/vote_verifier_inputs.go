package voteverifiertest

import (
	"crypto/ecdsa"
	"log"
	"math/big"
	"testing"
	"time"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

// VoterTestData struct includes the information required to generate the test
// inputs for the VerifyVoteCircuit.
type VoterTestData struct {
	PrivKey *ethereum.Signer
	PubKey  ecdsa.PublicKey
	Address common.Address
}

// VoteVerifierInputsForTest returns the VoteVerifierTestResults, the placeholder
// and the assignments for a VerifyVoteCircuit including the provided voters. If
// processId is nil, it will be randomly generated. Uses quicktest assertions
// instead of returning errors.
func VoteVerifierInputsForTest(
	t *testing.T,
	votersData []VoterTestData,
	processID *types.ProcessID,
	censusOrigin types.CensusOrigin,
) (
	circuitstest.VoteVerifierTestResults,
	voteverifier.VerifyVoteCircuit,
	[]voteverifier.VerifyVoteCircuit,
) {
	c := qt.New(t)

	now := time.Now()
	log.Println("voteVerifier inputs generation start")
	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(
		ballottest.TestCircomVerificationKey, circuits.BallotProofNPubInputs)
	c.Assert(err, qt.IsNil, qt.Commentf("circom placeholder"))

	// if no process ID is provided, use the centralized testing ProcessID
	if processID == nil {
		processID = types.TestProcessID
	}
	// Use deterministic encryption key for consistent caching
	ek := ballottest.GenDeterministicEncryptionKeyForTest(circuitstest.GenerateDeterministicSeed(processID, 0))
	encryptionKey := circuits.EncryptionKeyFromECCPoint(ek)
	// circuits assignments, voters data and proofs
	var assignments []voteverifier.VerifyVoteCircuit
	inputsHashes, addresses, weights, voteIDs := []*big.Int{}, []*big.Int{}, []*big.Int{}, []types.HexBytes{}
	ballots := []elgamal.Ballot{}
	var finalProcessID *big.Int
	for i, voter := range votersData {
		// Use deterministic ballot proof generation for consistent caching
		voterProof, err := ballottest.BallotProofForTestDeterministic(voter.Address.Bytes(), processID, ek, circuitstest.GenerateDeterministicSeed(processID, i+100))
		c.Assert(err, qt.IsNil, qt.Commentf("ballotproof inputs for voter %d", i))

		if finalProcessID == nil {
			finalProcessID = voterProof.ProcessID
		}
		addresses = append(addresses, voterProof.Address)
		weights = append(weights, big.NewInt(int64(circuits.MockWeight)))
		voteIDs = append(voteIDs, voterProof.VoteID)
		ballots = append(ballots, *voterProof.Ballot)
		// sign the inputs hash with the private key
		signature, err := ballottest.SignECDSAForTest(voter.PrivKey, voterProof.VoteID)
		c.Assert(err, qt.IsNil, qt.Commentf("sign ECDSA for voter %d", i))

		// hash the inputs of gnark circuit (except weight and including census root)
		inputsHash, err := voteverifier.VoteVerifierInputHash(
			voterProof.ProcessID,
			circuits.MockBallotMode(),
			encryptionKey,
			voterProof.Address,
			voterProof.VoteID,
			voterProof.Ballot.FromTEtoRTE(),
			censusOrigin,
		)
		c.Assert(err, qt.IsNil, qt.Commentf("vote verifier input hash for voter %d", i))

		inputsHashes = append(inputsHashes, inputsHash)
		// compose circuit placeholders
		recursiveProof, err := circomgnark.Circom2GnarkProofForRecursion(ballottest.TestCircomVerificationKey, voterProof.Proof, voterProof.PubInputs)
		c.Assert(err, qt.IsNil, qt.Commentf("circom to gnark proof for voter %d", i))

		assignments = append(assignments, voteverifier.VerifyVoteCircuit{
			IsValid:    1,
			InputsHash: emulated.ValueOf[sw_bn254.ScalarField](inputsHash),
			// circom inputs
			Vote: circuits.EmulatedVote[sw_bn254.ScalarField]{
				Address: emulated.ValueOf[sw_bn254.ScalarField](voterProof.Address),
				VoteID:  emulated.ValueOf[sw_bn254.ScalarField](voterProof.VoteID.BigInt().MathBigInt()),
				Ballot:  *voterProof.Ballot.FromTEtoRTE().ToGnarkEmulatedBN254(),
			},
			UserWeight: emulated.ValueOf[sw_bn254.ScalarField](circuits.MockWeight),
			Process: circuits.Process[emulated.Element[sw_bn254.ScalarField]]{
				ID:            emulated.ValueOf[sw_bn254.ScalarField](voterProof.ProcessID),
				CensusOrigin:  emulated.ValueOf[sw_bn254.ScalarField](censusOrigin.BigInt().MathBigInt()),
				EncryptionKey: encryptionKey.BigIntsToEmulatedElementBN254(),
				BallotMode:    circuits.MockBallotModeEmulated(),
			},
			// signature
			PublicKey: gnarkecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]{
				X: emulated.ValueOf[emulated.Secp256k1Fp](voter.PubKey.X),
				Y: emulated.ValueOf[emulated.Secp256k1Fp](voter.PubKey.Y),
			},
			Signature: gnarkecdsa.Signature[emulated.Secp256k1Fr]{
				R: emulated.ValueOf[emulated.Secp256k1Fr](signature.R),
				S: emulated.ValueOf[emulated.Secp256k1Fr](signature.S),
			},
			// circom proof
			CircomProof: recursiveProof.Proof,
		})
	}
	log.Printf("voteVerifier inputs generation ends, it tooks %s\n", time.Since(now))
	return circuitstest.VoteVerifierTestResults{
			InputsHashes:     inputsHashes,
			EncryptionPubKey: encryptionKey,
			Addresses:        addresses,
			Weights:          weights,
			ProcessID:        finalProcessID,
			CensusOrigin:     censusOrigin,
			Ballots:          ballots,
			VoteIDs:          voteIDs,
		}, voteverifier.VerifyVoteCircuit{
			CircomProof:           circomPlaceholder.Proof,
			CircomVerificationKey: circomPlaceholder.Vk,
		}, assignments
}
