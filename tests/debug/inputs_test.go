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
	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	ballottest "github.com/vocdoni/davinci-node/circuits/test/ballotproof"
	"github.com/vocdoni/davinci-node/circuits/voteverifier"
	"github.com/vocdoni/davinci-node/crypto"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
	"github.com/vocdoni/davinci-node/util/circomgnark"
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
	processID := crypto.BigToFF(params.BallotProofCurve.ScalarField(), process.ProcessID.BigInt().MathBigInt())
	root := arbo.BytesToBigInt(vote.CensusProof.Root)
	ballotMode := circuits.BallotModeToCircuit(process.BallotMode)
	encryptionKey := circuits.EncryptionKey[*big.Int]{
		PubKey: [2]*big.Int{
			process.EncryptionPubKey[0].MathBigInt(),
			process.EncryptionPubKey[1].MathBigInt(),
		},
	}

	// convert the circom proof to gnark proof and verify it
	err = ballotproof.Artifacts.LoadAll()
	c.Assert(err, qt.IsNil)
	ballotProof, err := circomgnark.VerifyAndConvertToRecursion(
		ballotproof.Artifacts.RawVerifyingKey(),
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
	hashInputs = append(hashInputs, rteBallot.BigInts()...)

	inputHash, err := mimc7.Hash(hashInputs, nil)
	c.Assert(err, qt.IsNil)

	signature := new(ethereum.ECDSASignature).SetBytes(vote.Signature)
	c.Assert(signature, qt.IsNotNil)
	signatureOk, pubkey := signature.VerifyBLS12377(vote.BallotInputsHash.MathBigInt(), common.BytesToAddress(vote.Address))
	c.Assert(signatureOk, qt.IsTrue)
	pubKey, err := ethcrypto.UnmarshalPubkey(pubkey)
	c.Assert(err, qt.IsNil)

	// Test the signature is correctly generated
	signer, err := ethereum.NewSignerFromHex("45d17557419bc5f4e1dab368badd10de5226667109239c0c613641e17ce5b03b")
	c.Assert(err, qt.IsNil)
	blsCircomInputsHash := crypto.BigIntToFFToSign(vote.BallotInputsHash.MathBigInt(), params.VoteVerifierCurve.ScalarField())
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
			Address: emulated.ValueOf[sw_bn254.ScalarField](vote.CensusProof.Address.BigInt().MathBigInt()),
			Ballot:  *rteBallot.ToGnarkEmulatedBN254(),
		},
		Process: circuits.Process[emulated.Element[sw_bn254.ScalarField]]{
			ID: emulated.ValueOf[sw_bn254.ScalarField](processID),
			// CensusRoot:    emulated.ValueOf[sw_bn254.ScalarField](root),
			EncryptionKey: encryptionKey.BigIntsToEmulatedElementBN254(),
			BallotMode:    ballotMode.BigIntsToEmulatedElementBN254(),
		},
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

	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(
		ballottest.TestCircomVerificationKey, circuits.BallotProofNPubInputs)
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
			params.AggregatorCurve.ScalarField(),
			params.VoteVerifierCurve.ScalarField())),
	)
}
