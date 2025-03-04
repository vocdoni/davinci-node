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
//   - MaxCount: The maximum number of votes that can be included in the
//     package.
//   - ForceUniqueness: A flag that indicates if the votes in the package
//     values should be unique.
//   - MaxValue: The maximum value that a vote can have.
//   - MinValue: The minimum value that a vote can have.
//   - MaxTotalCost: The maximum total cost of the votes in the package.
//   - MinTotalCost: The minimum total cost of the votes in the package.
//   - CostExp: The exponent used to calculate the cost of a vote.
//   - CostFromWeight: A flag that indicates if the cost of a vote is
//     calculated from the weight of the user or from the value of the vote.
//   - Address: The address of the voter.
//   - UserWeight: The weight of the user that is voting.
//   - EncryptionPubKey: The public key used to encrypt the votes in the
//     package.
//   - Nullifier: The nullifier of the votes in the package.
//   - Commitment: The commitment of the votes in the package.
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
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/recursion/groth16"
	"github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/vocdoni/gnark-crypto-primitives/emulated/bn254/twistededwards/mimc7"
	address "github.com/vocdoni/gnark-crypto-primitives/emulated/ecdsa"
	"github.com/vocdoni/gnark-crypto-primitives/tree/arbo"
	"github.com/vocdoni/gnark-crypto-primitives/utils"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
)

type VerifyVoteCircuit struct {
	IsValid frontend.Variable `gnark:",public"`
	// Single public input that is the hash of all the public inputs
	InputsHash emulated.Element[sw_bn254.ScalarField] `gnark:",public"`
	// The following variables are priv-public inputs, so should be hashed
	// and compared with the InputsHash or CircomPublicInputsHash. All the
	// variables should be hashed in the same order as they are defined here.

	// User public inputs
	Vote           circuits.EmulatedVote[sw_bn254.ScalarField]
	Process        circuits.Process[emulated.Element[sw_bn254.ScalarField]]
	UserWeight     emulated.Element[sw_bn254.ScalarField]
	CensusSiblings [circuits.CensusProofMaxLevels]emulated.Element[sw_bn254.ScalarField]
	// The following variables are private inputs and they are used to verify
	// the user identity ownership
	Msg       emulated.Element[emulated.Secp256k1Fr]
	PublicKey ecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]
	Signature ecdsa.Signature[emulated.Secp256k1Fr]
	// The ballot proof is passed as private inputs
	CircomProof           groth16.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine]
	CircomVerificationKey groth16.VerifyingKey[sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl] `gnark:"-"`
}

// censusKeyValue function converts the user address and weight to the current
// compiler field as variables to be used in the census proof. The address is
// converted to bytes and then to a variable to truncate it to 20 bytes. The
// weight is directly converted to a variable.
func censusKeyValue(api frontend.API, address, weight emulated.Element[sw_bn254.ScalarField]) (
	frontend.Variable, frontend.Variable, error,
) {
	// convert user address to bytes to swap the endianness
	bAddress, err := utils.ElemToU8(api, address)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to convert address emulated element to bytes: %w", err)
	}
	// swap the endianness of the address to le to be used in the census proof
	key, err := utils.U8ToVar(api, bAddress[:20])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to convert address bytes to var: %w", err)
	}
	// convert the user weight from the scalar field of the bn254 curve to the
	// current compiler field as a variable to be used in the census proof
	value, err := utils.PackScalarToVar(api, weight)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to convert weight emulated element to var: %w", err)
	}
	return key, value, nil
}

// circomWitness circuit method calculates the witness of the circom circuit
// with the public-private inputs provided by the user. It hashes the inputs
// and generates the unique public input of the circom circuit.
func (c VerifyVoteCircuit) circomWitness(api frontend.API) groth16.Witness[sw_bn254.ScalarField] {
	// hash the circom public-private inputs and compare them with the unique
	// public input of the circom circuit
	hFn, err := mimc7.NewMiMC(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create emulated MiMC hash function: ", err)
	}
	hFn.Write(circuits.CircomInputs(api, c.Process, c.Vote, c.UserWeight)...)
	return groth16.Witness[sw_bn254.ScalarField]{
		Public: []emulated.Element[sw_bn254.ScalarField]{hFn.Sum()},
	}
}

// checkInputsHash circuit method hashes the inputs provided by the user and
// compares them with the unique public input of the circuit. As a circuit
// method, it does not return any value, but it asserts that the hash of the
// inputs matches the unique public input of the circuit. The inputs hash is
// calculated by hashing all the private-public inputs provided by the user,
// including the census root, except the user weight and siblings (private
// inputs). The order of the inputs should match the order of the inputs of the
// circuit.
func (c VerifyVoteCircuit) checkInputsHash(api frontend.API) {
	// hash the inputs and compare them with the unique public input of the
	// circuit
	h, err := mimc7.NewMiMC(api)
	if err != nil {
		circuits.FrontendError(api, "failed to create hash function", err)
	}
	if err := h.Write(circuits.EmulatedVoteVerifierInputs(c.Process, c.Vote)...); err != nil {
		circuits.FrontendError(api, "failed to hash inputs", err)
	}
	flag := h.AssertSumIsEqualFlag(c.InputsHash)
	// if the inputs are valid, ensure that the result of the comparison is 1,
	// otherwise, the result does not matter so force it to be 1
	api.AssertIsEqual(api.Select(c.IsValid, flag, 1), 1)
}

// verifySigForAddress circuit method verifies the signature provided with the
// public key and message provided. It derives the address from the public key
// and verifies it matches the provided address. As a circuit method, it does
// not return any value, but it asserts that the signature is valid for the
// public key and message provided, and that the derived address matches the
// provided address.
func (c VerifyVoteCircuit) verifySigForAddress(api frontend.API) {
	// check the signature of the circom inputs hash provided as Secp256k1
	// emulated element
	validSign := c.PublicKey.SignIsValid(api, sw_emulated.GetCurveParams[emulated.Secp256k1Fp](), &c.Msg, &c.Signature)
	// if the inputs are valid, ensure that thre result of the verification
	// is 1, otherwise, the result does not matter so force it to be 1
	api.AssertIsEqual(api.Select(c.IsValid, validSign, 1), 1)
	// derive the address from the public key and check it matches the provided
	// address
	derivedAddr, err := address.DeriveAddress(api, c.PublicKey)
	if err != nil {
		circuits.FrontendError(api, "failed to derive address", err)
	}
	// convert the derived address from the scalar field of the bn254 curve to
	// the current compiler field as a variable to compare it with the address
	// derived from the public key and to be used in the census proof
	address, err := utils.PackScalarToVar(api, c.Vote.Address)
	if err != nil {
		circuits.FrontendError(api, "failed to convert emulated address to var", err)
	}
	// if the proof is not valid force the derived address to be equal to the
	// provided address
	derivedAddr = api.Select(c.IsValid, derivedAddr, address)
	api.AssertIsEqual(address, derivedAddr)
}

// verifyCircomProof circuit method verifies the ballot proof provided by the
// user. It uses the verification key provided by the user to verify the proof
// over the bn254 curve. As a circuit method, it does not return any value, but
// it asserts that the proof is valid for the public inputs provided by the
// user.
func (c VerifyVoteCircuit) verifyCircomProof(api frontend.API) {
	// verify the ballot proof over the bn254 curve (used by circom)
	verifier, err := groth16.NewVerifier[sw_bn254.ScalarField, sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl](api)
	if err != nil {
		circuits.FrontendError(api, "failed to create BN254 verifier", err)
	}
	validProof, err := verifier.ProofIsValid(c.CircomVerificationKey, c.CircomProof,
		c.circomWitness(api), groth16.WithCompleteArithmetic(),
	)
	if err != nil {
		circuits.FrontendError(api, "failed to verify circom proof", err)
		api.AssertIsEqual(0, 1)
	}
	// if the inputs are valid, ensure that the result of the verification is 1,
	// otherwise, the result does not matter so force it to be 1
	api.AssertIsEqual(api.Select(c.IsValid, validProof, 1), 1)
}

// verifyCensusProof circuit method verifies the census proof provided by the
// user. It uses the root and siblings provided by the user to verify the proof
// over the current compiler field. As a circuit method, it does not return any
// value, but it asserts that the proof is valid for the address and user weight
// provided by the user. The census key and value comes from the address and
// user weight provided by the user.
func (c VerifyVoteCircuit) verifyCensusProof(api frontend.API) {
	key, value, err := censusKeyValue(api, c.Vote.Address, c.UserWeight)
	if err != nil {
		circuits.FrontendError(api, "failed to get census key-value", err)
	}
	// convert emulated census root and siblings to native variables
	root, err := utils.PackScalarToVar(api, c.Process.CensusRoot)
	if err != nil {
		circuits.FrontendError(api, "failed to convert emulated root to var", err)
	}
	siblings := make([]frontend.Variable, len(c.CensusSiblings))
	for i, sibling := range c.CensusSiblings {
		siblings[i], err = utils.PackScalarToVar(api, sibling)
		if err != nil {
			circuits.FrontendError(api, "failed to convert emulated sibling to var", err)
		}
	}
	// verify the census proof using the derived address and the user weight
	// provided as leaf key-value, adn the root and siblings provided
	flag := arbo.CheckInclusionProofFlag(api, utils.MiMCHasher, key, value, root, siblings)
	// if the inputs are valid, ensure that the result of the verification is 1,
	// otherwise, the result does not matter so force it to be 1
	api.AssertIsEqual(api.Select(c.IsValid, flag, 1), 1)
}

func (c VerifyVoteCircuit) Define(api frontend.API) error {
	// check the hash of the inputs provided by the user
	c.checkInputsHash(api)
	// verify the signature of the public inputs
	c.verifySigForAddress(api)
	// verify the census proof
	c.verifyCensusProof(api)
	// verify the ballot proof
	c.verifyCircomProof(api)
	return nil
}
