package debug

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	stdgroth16 "github.com/consensys/gnark/std/recursion/groth16"
	gnarkecdsa "github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/consensys/gnark/test"
	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	qt "github.com/frankban/quicktest"
	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/api"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/ballotproof"
	ballottest "github.com/vocdoni/vocdoni-z-sandbox/circuits/test/ballotproof"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/voteverifier"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/signatures/ethereum"
	"github.com/vocdoni/vocdoni-z-sandbox/storage/census"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func TestDebugVoteVerifier(t *testing.T) {
	if os.Getenv("DEBUG") == "" || os.Getenv("DEBUG") == "false" {
		t.Skip("skipping debug tests...")
	}
	c := qt.New(t)

	// open process setup
	processSetup, err := os.ReadFile("./process_setup.json")
	c.Assert(err, qt.IsNil)
	process := &types.ProcessSetupResponse{}
	err = json.Unmarshal(processSetup, process)
	c.Assert(err, qt.IsNil)
	// open vote inputs
	debugInputs, err := os.ReadFile("./vote_inputs.json")
	c.Assert(err, qt.IsNil)
	vote := &api.Vote{}
	err = json.Unmarshal(debugInputs, vote)
	c.Assert(err, qt.IsNil)

	// decode info
	processID := crypto.BigToFF(circuits.BallotProofCurve.ScalarField(), process.ProcessID.BigInt().MathBigInt())
	root := arbo.BytesToBigInt(vote.CensusProof.Root)
	ballotMode := circuits.BallotModeToCircuit(process.BallotMode)
	encKey := types.EncryptionKey{
		X: process.EncryptionPubKey[0],
		Y: process.EncryptionPubKey[1],
	}
	encryptionKey := circuits.EncryptionKeyToCircuit(encKey)

	// convert the circom proof to gnark proof and verify it
	err = ballotproof.Artifacts.LoadAll()
	c.Assert(err, qt.IsNil)
	ballotProof, err := circuits.VerifyAndConvertToRecursion(
		ballotproof.Artifacts.VerifyingKey(),
		vote.BallotProof,
		[]string{vote.BallotInputsHash.String()},
	)
	c.Assert(err, qt.IsNil)
	// convert the ballots from TE (circom) to RTE (gnark)
	rteBallot := vote.Ballot.FromTEtoRTE()
	// Calculate vote verifier inputs hash
	hashInputs := make([]*big.Int, 0, 8+len(vote.Ballot.BigInts()))
	hashInputs = append(hashInputs, processID)
	hashInputs = append(hashInputs, root)
	hashInputs = append(hashInputs, ballotMode.Serialize()...)
	hashInputs = append(hashInputs, encryptionKey.Serialize()...)
	hashInputs = append(hashInputs, vote.Address.BigInt().MathBigInt())
	hashInputs = append(hashInputs, vote.Commitment.MathBigInt())
	hashInputs = append(hashInputs, vote.Nullifier.MathBigInt())
	hashInputs = append(hashInputs, rteBallot.BigInts()...)

	inputHash, err := mimc7.Hash(hashInputs, nil)
	c.Assert(err, qt.IsNil)

	siblings, err := census.BigIntSiblings(vote.CensusProof.Siblings)
	c.Assert(err, qt.IsNil)

	emulatedSiblings := [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
	for i, s := range circuits.BigIntArrayToN(siblings, types.CensusTreeMaxLevels) {
		emulatedSiblings[i] = emulated.ValueOf[sw_bn254.ScalarField](s)
	}

	signature := new(ethereum.ECDSASignature).SetBytes(vote.Signature)
	c.Assert(signature, qt.IsNotNil)
	signatureOk, pubkey := signature.VerifyBLS12377(vote.BallotInputsHash.MathBigInt(), common.BytesToAddress(vote.Address))
	c.Assert(signatureOk, qt.IsTrue)
	pubKey, err := ethcrypto.UnmarshalPubkey(pubkey)
	c.Assert(err, qt.IsNil)

	// Test the signature is correctly generated
	signer, err := ethereum.NewSignerFromHex("45d17557419bc5f4e1dab368badd10de5226667109239c0c613641e17ce5b03b")
	c.Assert(err, qt.IsNil)
	blsCircomInputsHash := crypto.BigIntToFFwithPadding(vote.BallotInputsHash.MathBigInt(), circuits.VoteVerifierCurve.ScalarField())
	localSignature, err := signer.Sign(blsCircomInputsHash)
	c.Assert(err, qt.IsNil)
	c.Assert(localSignature.R.String(), qt.DeepEquals, signature.R.String(), qt.Commentf("signature.R"))
	c.Assert(localSignature.S.String(), qt.DeepEquals, signature.S.String(), qt.Commentf("signature.S"))

	// Compare pubkeys
	c.Assert(pubKey.X.String(), qt.DeepEquals, signer.X.String(), qt.Commentf("pubkey.X"))
	c.Assert(pubKey.Y.String(), qt.DeepEquals, signer.Y.String(), qt.Commentf("pubkey.Y"))

	assignment := voteverifier.VerifyVoteCircuit{
		IsValid:    1,
		InputsHash: emulated.ValueOf[sw_bn254.ScalarField](inputHash),
		Vote: circuits.EmulatedVote[sw_bn254.ScalarField]{
			Address:    emulated.ValueOf[sw_bn254.ScalarField](vote.CensusProof.Key.BigInt().MathBigInt()),
			Commitment: emulated.ValueOf[sw_bn254.ScalarField](vote.Commitment.MathBigInt()),
			Nullifier:  emulated.ValueOf[sw_bn254.ScalarField](vote.Nullifier.MathBigInt()),
			Ballot:     *rteBallot.ToGnarkEmulatedBN254(),
		},
		UserWeight: emulated.ValueOf[sw_bn254.ScalarField](vote.CensusProof.Weight.MathBigInt()),
		Process: circuits.Process[emulated.Element[sw_bn254.ScalarField]]{
			ID:            emulated.ValueOf[sw_bn254.ScalarField](processID),
			CensusRoot:    emulated.ValueOf[sw_bn254.ScalarField](root),
			EncryptionKey: encryptionKey.BigIntsToEmulatedElementBN254(),
			BallotMode:    ballotMode.BigIntsToEmulatedElementBN254(),
		},
		CensusSiblings: emulatedSiblings,
		PublicKey: gnarkecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]{
			X: emulated.ValueOf[emulated.Secp256k1Fp](pubKey.X),
			Y: emulated.ValueOf[emulated.Secp256k1Fp](pubKey.Y),
		},
		Signature: gnarkecdsa.Signature[emulated.Secp256k1Fr]{
			R: emulated.ValueOf[emulated.Secp256k1Fr](signature.R),
			S: emulated.ValueOf[emulated.Secp256k1Fr](signature.S),
		},
		CircomProof: ballotProof.Proof,
	}

	circomPlaceholder, err := circuits.Circom2GnarkPlaceholder(ballottest.TestCircomVerificationKey)
	c.Assert(err, qt.IsNil)

	placeholder := voteverifier.VerifyVoteCircuit{
		CircomProof:           circomPlaceholder.Proof,
		CircomVerificationKey: circomPlaceholder.Vk,
	}
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&placeholder, &assignment,
		test.WithCurves(ecc.BLS12_377),
		test.WithBackends(backend.GROTH16),
		test.WithProverOpts(stdgroth16.GetNativeProverOptions(
			circuits.AggregatorCurve.ScalarField(),
			circuits.VoteVerifierCurve.ScalarField())),
	)
}
