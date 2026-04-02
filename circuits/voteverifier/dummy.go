package voteverifier

import (
	"math/big"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util/circomgnark"
)

const (
	dummyBallotProof = `{
 "pi_a": [
  "11711080065308007682838320732817046446099838935738802330966049640254691191206",
  "17850983632338003012778437738834870617367310716158639359113239661166347758019",
  "1"
 ],
 "pi_b": [
  [
   "8825235276620994813470418380223243496774006915527119606983048301671588119024",
   "2226303267471545357145519696076194402397717063319440340904386422082448596035"
  ],
  [
   "8568618036867104573055602703586133087480374891027401247685709167152098077693",
   "1803786236097892649915632611783799060891816616100592289329778375159898063175"
  ],
  [
   "1",
   "0"
  ]
 ],
 "pi_c": [
  "443251187306603655641512095920183574737557831206616603914644748264416016054",
  "4110411832118690910191887320272248494012149664813960539989768130756673868858",
  "1"
 ],
 "protocol": "groth16",
 "curve": "bn128"
}`
	dummyBallotPubInputs = `[
 "1220176476709744867553669165001455267652926745576",
 "1150356464581538673947970931497476016316344861907",
 "16723164540749581091497422535343559644999010121456604327047742204957502303153"
]`
)

var (
	// Keep this tuple internally consistent: address, public key, and signature
	// belong to VoteID=1 and are used only to build the canonical dummy witness.
	dummyAddress    = mustBigIntHex("ad58a7233b993fd9588dfbdd614fe84ebb2dff0e")
	dummyPublicKeyX = mustBigIntHex("e1786f8f6da8e998329c266d1b01cae8552cfcc1d2037bf9bb647ff357d49bc9")
	dummyPublicKeyY = mustBigIntHex("a17b6c8959b29dfd7f8222ff65425bd6f081001e20083576c6b61e684ba4ac9b")
	dummySignatureR = mustBigIntHex("038eedd337064465cc3ce29fc5d240d1346e659e2fe040b36a7da8b4f91e98d9")
	dummySignatureS = mustBigIntHex("0c920de03fd767d3e945d3192b17febbcf4c230482b8f83292ddc25c565473ae")
)

func mustBigIntHex(s string) *big.Int {
	v, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("invalid big.Int hex constant")
	}
	return v
}

// DummyPlaceholder function returns a placeholder for the VerifyVoteCircuit
// with dummy values. This function can be used to generate
// dummy proofs to fill a chunk of votes that does not reach the required number
// of votes to be valid.
func DummyPlaceholder() (*VerifyVoteCircuit, error) {
	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(ballotproof.CircomVerificationKey, ballotproof.NumberOfPublicInputs)
	if err != nil {
		return nil, err
	}
	return &VerifyVoteCircuit{
		CircomProof:           circomPlaceholder.Proof,
		CircomVerificationKey: circomPlaceholder.Vk,
	}, nil
}

// DummyAssignment function returns a dummy assignment for the VerifyVoteCircuit
// with dummy values.
// This function can be used to generate dummy proofs to fill a chunk of votes
// that does not reach the required number of votes to be valid.
func DummyAssignment() (*VerifyVoteCircuit, error) {
	recursiveProof, err := circomgnark.Circom2GnarkProofForRecursion(ballotproof.CircomVerificationKey, dummyBallotProof, dummyBallotPubInputs)
	if err != nil {
		return nil, err
	}
	// dummy values
	dummyEmulatedBN254 := emulated.ValueOf[sw_bn254.ScalarField](1)
	return &VerifyVoteCircuit{
		IsValid:    0,
		BallotHash: dummyEmulatedBN254,
		Address:    emulated.ValueOf[sw_bn254.ScalarField](dummyAddress),
		VoteID:     types.VoteID(1).BigInt(),
		PublicKey: ecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]{
			X: emulated.ValueOf[emulated.Secp256k1Fp](dummyPublicKeyX),
			Y: emulated.ValueOf[emulated.Secp256k1Fp](dummyPublicKeyY),
		},
		Signature: ecdsa.Signature[emulated.Secp256k1Fr]{
			R: emulated.ValueOf[emulated.Secp256k1Fr](dummySignatureR),
			S: emulated.ValueOf[emulated.Secp256k1Fr](dummySignatureS),
		},
		CircomProof: recursiveProof.Proof,
	}, nil
}
