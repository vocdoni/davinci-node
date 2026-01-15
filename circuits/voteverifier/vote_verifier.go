// voteverifier package contains the Gnark circuit definition that verifies a
// vote package to be aggregated by the vote aggregator and included in a new
// state transition. A vote package includes a ballot proof (generated from
// a circom circuit with snarkjs), the public inputs of the ballot proof
// circuit, the signature of the public inputs, and a census proof. The vote
// package is valid if the ballot proof is valid if:
//   - The public inputs of the ballot proof are valid (match with the hash
//     provided).
//   - The ballot proof is valid for the public inputs.
//   - The public inputs of the verification circuit are valid (match with the
//     hash provided).
//   - The signature of the public inputs is valid for the public key of the
//     voter.
//   - The address derived from the user public key is part of the census, and
//     verifies the census proof with the user weight provided.
//
// Public inputs:
//   - InputsHash: The hash of all the inputs that could be public.
//
// Private inputs:
//   - NumFields: The maximum number of votes that can be included in the
//     package.
//   - UniqueValues: A flag that indicates if the votes in the package
//     values should be unique.
//   - MaxValue: The maximum value that a vote can have.
//   - MinValue: The minimum value that a vote can have.
//   - MaxValueSum: The maximum total cost of the votes in the package.
//   - MinValueSum: The minimum total cost of the votes in the package.
//   - CostExponent: The exponent used to calculate the cost of a vote.
//   - CostFromWeight: A flag that indicates if the cost of a vote is
//     calculated from the weight of the user or from the value of the vote.
//   - Address: The address of the voter.
//   - UserWeight: The weight of the user that is voting.
//   - EncryptionPubKey: The public key used to encrypt the votes in the
//     package.
//   - ProcessId: The process id of the votes in the package.
//   - Ballot: The encrypted votes in the package.
//   - CensusRoot: The root of the census tree.
//   - CensusSiblings: The siblings of the address in the census tree.
//   - Msg: The hash of the public inputs of the ballot proof but as scalar
//     element of the Secp256k1 curve.
//   - PublicKey: The public key of the voter.
//   - Signature: The signature of the inputs hash.
//   - CircomProof: The proof of the ballot proof.
//   - CircomPublicInputsHash: The hash of the public inputs of the ballot proof.
//   - CircomVerificationKey: The verification key of the ballot proof (fixed).
//
// Note: The inputs of the circom circuit should be provided as elements of
// the bn254 scalar field, and the inputs of the gnark circuit should be
// provided as elements of the current compiler field (BLS12377 expected).
package voteverifier

import (
	"fmt"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_emulated"
	"github.com/consensys/gnark/std/hash/sha3"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	address "github.com/vocdoni/gnark-crypto-primitives/emulated/ecdsa"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
)

type VerifyVoteCircuit struct {
	IsValid frontend.Variable `gnark:",public"`

	// Hash of the public inputs of the ballot proof circuit (circom)
	BallotHash emulated.Element[sw_bn254.ScalarField] `gnark:",public"`

	// The following variables are private inputs and they are used to verify
	// the user identity ownership
	Address   emulated.Element[sw_bn254.ScalarField]
	VoteID    frontend.Variable
	PublicKey ecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]
	Signature ecdsa.Signature[emulated.Secp256k1Fr]
	// The ballot proof is passed as private inputs
	CircomProof           groth16.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine]
	CircomVerificationKey groth16.VerifyingKey[sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl] `gnark:"-"`
}

// verifySigForAddress circuit method verifies the signature provided with the
// public key and message provided. It derives the address from the public key
// and verifies it matches the provided address. As a circuit method, it does
// not return any value, but it asserts that the signature is valid for the
// public key and voteID provided, and that the derived address matches the
// provided address.
func (c *VerifyVoteCircuit) verifySigForAddress(api frontend.API) {
	// we need to prefix the message with the Ethereum signing prefix and the
	// length of the message before hashing it, so we need to convert the
	// ethereum prefix to bytes and append the length of the message
	prefix := utils.BytesFromString(fmt.Sprintf("%s%d", ethereum.SigningPrefix, ethereum.HashLength), len(ethereum.SigningPrefix)+2)
	// convert the voteID to emulated secp256k1 field for signature verification
	msgSecp256, err := utils.UnpackVarToScalar[emulated.Secp256k1Fr](api, c.VoteID)
	if err != nil {
		circuits.FrontendError(api, "failed to unpack voteID", err)
	}
	// first convert the message to bytes and swap the endianness of the content (the hash of the data to be signed)
	content, err := utils.BytesFromElement(api, *msgSecp256)
	if err != nil {
		circuits.FrontendError(api, "failed to convert circomHash to bytes", err)
	}
	content = utils.SwapEndianness(content)
	// concatenate the prefix and content to create the hash to be signed
	msg := utils.Bytes(append(prefix[:], content[:]...))
	keccak, err := sha3.NewLegacyKeccak256(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create hash function", err)
	}
	keccak.Write(msg)
	// we need to swap the endianess again and convert the bytes back to the emulated secp256k1 field
	hash := utils.SwapEndianness(keccak.Sum())
	emulatedHash, err := utils.U8ToElem[emulated.Secp256k1Fr](api, hash)
	if err != nil {
		circuits.FrontendError(api, "failed to convert hash to emulated element", err)
	}
	// check the signature of the circom inputs hash provided as Secp256k1 emulated element
	validSign := c.PublicKey.IsValid(api, sw_emulated.GetCurveParams[emulated.Secp256k1Fp](), &emulatedHash, &c.Signature)
	// if the inputs are valid, ensure that thre result of the verification
	// is 1, otherwise, the result does not matter so force it to be 1
	api.AssertIsEqual(api.Select(c.IsValid, validSign, 1), 1)
	// derive the address from the public key and check it matches the provided
	// address
	derivedAddr, err := address.DeriveAddress(api, c.PublicKey)
	if err != nil {
		circuits.FrontendError(api, "failed to derive address", err)
	}
	// Convert the emulated address to a variable for comparison
	addressVar, err := utils.PackScalarToVar(api, c.Address)
	if err != nil {
		circuits.FrontendError(api, "failed to convert address to var", err)
	}
	// if the proof is not valid force the derived address to be equal to the
	// provided address
	derivedAddr = api.Select(c.IsValid, derivedAddr, addressVar)
	api.AssertIsEqual(addressVar, derivedAddr)
}

// verifyCircomProof circuit method verifies the ballot proof provided by the
// user. It uses the verification key provided by the user to verify the proof
// over the bn254 curve. As a circuit method, it does not return any value, but
// it asserts that the proof is valid for the public inputs provided by the
// user.
func (c *VerifyVoteCircuit) verifyCircomProof(api frontend.API) {
	// calculate the hash of the circom circuit inputs
	witness := groth16.Witness[sw_bn254.ScalarField]{
		Public: []emulated.Element[sw_bn254.ScalarField]{c.BallotHash},
	}
	// verify the ballot proof over the bn254 curve (used by circom)
	verifier, err := groth16.NewVerifier[sw_bn254.ScalarField, sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl](api)
	if err != nil {
		circuits.FrontendError(api, "failed to create BN254 verifier", err)
	}
	validProof, err := verifier.IsValidProof(c.CircomVerificationKey, c.CircomProof,
		witness, groth16.WithCompleteArithmetic())
	if err != nil {
		circuits.FrontendError(api, "failed to verify circom proof", err)
		api.AssertIsEqual(0, 1)
	}
	// if the inputs are valid, ensure that the result of the verification is 1,
	// otherwise, the result does not matter so force it to be 1
	api.AssertIsEqual(api.Select(c.IsValid, validProof, 1), 1)
}

func (c *VerifyVoteCircuit) Define(api frontend.API) error {
	// verify the signature of the public inputs
	c.verifySigForAddress(api)
	// verify the ballot proof
	c.verifyCircomProof(api)
	return nil
}
