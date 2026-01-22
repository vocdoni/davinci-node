package voteverifier

import (
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/spec/params"
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

// DummyPlaceholder function returns a placeholder for the VerifyVoteCircuit
// with dummy values. It needs the desired BallotProof circuit verification key
// to generate inner circuit placeholders. This function can be used to generate
// dummy proofs to fill a chunk of votes that does not reach the required number
// of votes to be valid.
func DummyPlaceholder(ballotProofVKey []byte) (*VerifyVoteCircuit, error) {
	circomPlaceholder, err := circomgnark.Circom2GnarkPlaceholder(ballotProofVKey, circuits.BallotProofNPubInputs)
	if err != nil {
		return nil, err
	}
	return &VerifyVoteCircuit{
		CircomProof:           circomPlaceholder.Proof,
		CircomVerificationKey: circomPlaceholder.Vk,
	}, nil
}

// DummyAssignment function returns a dummy assignment for the VerifyVoteCircuit
// with dummy values. It needs the desired BallotProof circuit verification key
// and the curve of the points used for the ballots to generate the assignment.
// This function can be used to generate dummy proofs to fill a chunk of votes
// that does not reach the required number of votes to be valid.
func DummyAssignment(ballotProofVKey []byte, curve ecc.Point) (*VerifyVoteCircuit, error) {
	recursiveProof, err := circomgnark.Circom2GnarkProofForRecursion(ballotProofVKey, dummyBallotProof, dummyBallotPubInputs)
	if err != nil {
		return nil, err
	}
	// dummy values
	dummyEmulatedBN254 := emulated.ValueOf[sw_bn254.ScalarField](1)
	dummyEmulatedSecp256k1Fp := emulated.ValueOf[emulated.Secp256k1Fp](1)
	dummyEmulatedSecp256k1Fr := emulated.ValueOf[emulated.Secp256k1Fr](1)
	return &VerifyVoteCircuit{
		IsValid:    0,
		BallotHash: dummyEmulatedBN254,
		Address:    dummyEmulatedBN254,
		VoteID:     1,
		PublicKey: ecdsa.PublicKey[emulated.Secp256k1Fp, emulated.Secp256k1Fr]{
			X: dummyEmulatedSecp256k1Fp,
			Y: dummyEmulatedSecp256k1Fp,
		},
		Signature: ecdsa.Signature[emulated.Secp256k1Fr]{
			R: dummyEmulatedSecp256k1Fr,
			S: dummyEmulatedSecp256k1Fr,
		},
		CircomProof: recursiveProof.Proof,
	}, nil
}

// DummyWitness function returns a dummy witness for the VerifyVoteCircuit
// with dummy values. It needs the desired BallotProof circuit verification key
// and the curve of the points used for the ballots to generate the witness.
// This function can be used to generate dummy proofs to fill a chunk of votes
// that does not reach the required number of votes to be valid.
func DummyWitness(ballotProofVKey []byte, curve ecc.Point) (witness.Witness, error) {
	assignment, err := DummyAssignment(ballotProofVKey, curve)
	if err != nil {
		return nil, err
	}
	return frontend.NewWitness(assignment, params.VoteVerifierCurve.ScalarField())
}
