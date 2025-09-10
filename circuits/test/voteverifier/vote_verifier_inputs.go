package voteverifiertest

import (
	"crypto/ecdsa"
	"fmt"
	"log"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/circuits"
	circuitstest "github.com/vocdoni/davinci-node/circuits/test"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
	primitivestest "github.com/vocdoni/gnark-crypto-primitives/testutil"
)

const testCSPSeed = "1f1e0cd27b4ecd1b71b6333790864ace2870222c"

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
	var censusRoot *big.Int
	var censusSiblings [][types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]
	var cspProofs []csp.CSPProof
	switch censusOrigin {
	case types.CensusOriginMerkleTree:
		// generate a test census
		censusRoot, censusSiblings, cspProofs, err = CensusProofMerkleTree(votersData, processID)
		c.Assert(err, qt.IsNil, qt.Commentf("census proof merkle tree"))
	case types.CensusOriginCSPEdDSABLS12377:
		// generate a test census with CSP proofs
		censusRoot, censusSiblings, cspProofs, err = CensusProofCSP(votersData, processID, censusOrigin)
		c.Assert(err, qt.IsNil, qt.Commentf("census proof CSP"))
	default:
		c.Assert(false, qt.IsTrue, qt.Commentf("invalid census origin: %s", censusOrigin))
	}
	// Use deterministic encryption key for consistent caching
	ek := ballottest.GenDeterministicEncryptionKeyForTest(circuitstest.GenerateDeterministicSeed(processID, 0))
	encryptionKey := circuits.EncryptionKeyFromECCPoint(ek)
	// circuits assignments, voters data and proofs
	var assignments []voteverifier.VerifyVoteCircuit
	inputsHashes, addresses, voteIDs := []*big.Int{}, []*big.Int{}, []types.HexBytes{}
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
			censusRoot,
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
				CensusRoot:    emulated.ValueOf[sw_bn254.ScalarField](censusRoot),
				EncryptionKey: encryptionKey.BigIntsToEmulatedElementBN254(),
				BallotMode:    circuits.MockBallotModeEmulated(),
			},
			CensusSiblings: censusSiblings[i],
			CSPProof:       cspProofs[i],
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
			ProcessID:        finalProcessID,
			CensusOrigin:     censusOrigin,
			CensusRoot:       censusRoot,
			Ballots:          ballots,
			VoteIDs:          voteIDs,
		}, voteverifier.VerifyVoteCircuit{
			CircomProof:           circomPlaceholder.Proof,
			CircomVerificationKey: circomPlaceholder.Vk,
		}, assignments
}

func CensusProofMerkleTree(votersData []VoterTestData, processID *types.ProcessID) (
	*big.Int,
	[][types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField],
	[]csp.CSPProof,
	error,
) {
	bAddresses, bWeights := [][]byte{}, [][]byte{}
	for _, voter := range votersData {
		bAddresses = append(bAddresses, voter.Address.Bytes())
		bWeights = append(bWeights, new(big.Int).SetInt64(int64(circuits.MockWeight)).Bytes())
	}

	// Create a unique directory name to avoid lock conflicts
	// Include timestamp and process info for uniqueness
	timestamp := time.Now().UnixNano()
	seed := circuitstest.GenerateDeterministicSeed(processID, 999)
	censusDir := fmt.Sprintf("../assets/census_%d_%d", seed%1000, timestamp)

	// Ensure the assets directory exists
	if err := os.MkdirAll("../assets", 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create assets directory: %w", err)
	}

	// Clean up the census directory when done
	defer func() {
		if err := os.RemoveAll(censusDir); err != nil {
			log.Printf("Warning: failed to cleanup census directory %s: %v", censusDir, err)
		}
	}()

	testCensus, err := primitivestest.GenerateCensusProofLE(primitivestest.CensusTestConfig{
		Dir:           censusDir,
		ValidSiblings: 10,
		TotalSiblings: types.CensusTreeMaxLevels,
		KeyLen:        types.CensusKeyMaxLen,
		Hash:          arbo.HashFunctionMiMC_BLS12_377,
		BaseField:     arbo.BLS12377BaseField,
	}, bAddresses, bWeights)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to generate census proof: %w", err)
	}

	// transform siblings to gnark frontend.Variable
	emulatedSiblings := [][types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
	cspProofs := []csp.CSPProof{}
	for _, censusProof := range testCensus.Proofs {
		proofSiblings := [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
		for j, s := range censusProof.Siblings {
			proofSiblings[j] = emulated.ValueOf[sw_bn254.ScalarField](s)
		}
		emulatedSiblings = append(emulatedSiblings, proofSiblings)
		cspProofs = append(cspProofs, voteverifier.DummyCSPProof())
	}
	return testCensus.Root, emulatedSiblings, cspProofs, nil
}

func CensusProofCSP(votersData []VoterTestData, processID *types.ProcessID, censusOrigin types.CensusOrigin) (
	*big.Int,
	[][types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField],
	[]csp.CSPProof,
	error,
) {
	eddsaCSP, err := csp.New(censusOrigin, []byte(testCSPSeed))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create csp: %w", err)
	}
	emulatedSiblings := [][types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
	cspProofs := []csp.CSPProof{}
	for _, data := range votersData {
		emulatedSiblings = append(emulatedSiblings, voteverifier.DummySiblings())

		cspProof, err := eddsaCSP.GenerateProof(processID, data.Address)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to generate census proof: %w", err)
		}
		gnarkCSPProof, err := csp.CensusProofToCSPProof(cspProof)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to convert census proof to gnark proof: %w", err)
		}
		cspProofs = append(cspProofs, *gnarkCSPProof)
	}
	root := eddsaCSP.CensusRoot()
	return root.BigInt().MathBigInt(), emulatedSiblings, cspProofs, nil
}
