package voteverifier

import (
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/signature/ecdsa"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

const (
	dummyBallotProof     = `{"pi_a":["14317803560450518007258622782079415124606968983609047458603560773139573642372","13220836507919417810102500022053425847740793328680001481379588806373283435361","1"],"pi_b":[["13131863189118794736690050998331043576859490243826740722208778384318162975934","18778894917155800116078575126586542650582442463212848856829492469703094920614"],["5945565767923009887391492296076444978379707852528868231882627488814132645166","20656224691041450440102411065063016103413397995305538083438605043395017567283"],["1","0"]],"pi_c":["18636900127581479314580037530074764023654009190543056388408062565167626098061","5693628415107779555184896561874457697545026007594250545815021277644031507852","1"],"protocol":"groth16"}`
	dummyBallotPubInputs = `["3606337145402298579036699230083186742509778949285840724557626377163275164810"]`
)

// DummyPlaceholder function returns a placeholder for the VerifyVoteCircuit
// with dummy values. It needs the desired BallotProof circuit verification key
// to generate inner circuit placeholders. This function can be used to generate
// dummy proofs to fill a chunk of votes that does not reach the required number
// of votes to be valid.
func DummyPlaceholder(ballotProofVKey []byte) (*VerifyVoteCircuit, error) {
	circomPlaceholder, err := circuits.Circom2GnarkPlaceholder(ballotProofVKey)
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
	recursiveProof, err := circuits.Circom2GnarkProofForRecursion(ballotProofVKey, dummyBallotProof, dummyBallotPubInputs)
	if err != nil {
		return nil, err
	}
	// dummy values
	dummyEmulatedBN254 := emulated.ValueOf[sw_bn254.ScalarField](1)
	dummyEmulatedSecp256k1Fp := emulated.ValueOf[emulated.Secp256k1Fp](1)
	dummyEmulatedSecp256k1Fr := emulated.ValueOf[emulated.Secp256k1Fr](1)
	dummyEmulatedSiblings := [types.CensusTreeMaxLevels]emulated.Element[sw_bn254.ScalarField]{}
	for i := range dummyEmulatedSiblings {
		dummyEmulatedSiblings[i] = dummyEmulatedBN254
	}
	return &VerifyVoteCircuit{
		IsValid:    0,
		InputsHash: dummyEmulatedBN254,
		Vote: circuits.EmulatedVote[sw_bn254.ScalarField]{
			Address: dummyEmulatedBN254,
			VoteID:  dummyEmulatedBN254,
			Ballot:  *elgamal.NewBallot(curve).ToGnarkEmulatedBN254(),
		},
		UserWeight: dummyEmulatedBN254,
		Process: circuits.Process[emulated.Element[sw_bn254.ScalarField]]{
			ID:         dummyEmulatedBN254,
			CensusRoot: dummyEmulatedBN254,
			EncryptionKey: circuits.EncryptionKey[emulated.Element[sw_bn254.ScalarField]]{
				PubKey: [2]emulated.Element[sw_bn254.ScalarField]{dummyEmulatedBN254, dummyEmulatedBN254},
			},
			BallotMode: circuits.MockBallotModeEmulated(),
		},
		CensusSiblings: dummyEmulatedSiblings,
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
	return frontend.NewWitness(assignment, circuits.VoteVerifierCurve.ScalarField())
}
