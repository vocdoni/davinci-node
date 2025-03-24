package storage

import (
	"math/big"

	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	groth16_bw6761 "github.com/consensys/gnark/backend/groth16/bw6-761"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	recursion "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

type Process struct {
	CensusRoot    types.HexBytes   `json:"censusRoot"`
	BallotMode    types.BallotMode `json:"ballotMode"`
	MetadataHash  types.HexBytes   `json:"metadataID"`
	EncryptionKey EncryptionKeys   `json:"encryptionKey"`
}

type EncryptionKeys struct {
	X          *big.Int `json:"publicKeyX"`
	Y          *big.Int `json:"publicKeyY"`
	PrivateKey *big.Int `json:"-"`
}

type VerifiedBallot struct {
	ProcessID       types.HexBytes          `json:"processId"`
	VoterWeight     *big.Int                `json:"voterWeight"`
	Nullifier       *big.Int                `json:"nullifier"`
	Commitment      *big.Int                `json:"commitment"`
	EncryptedBallot elgamal.Ballot          `json:"encryptedBallot"`
	Address         *big.Int                `json:"address"`
	InputsHash      *big.Int                `json:"inputsHash"`
	Proof           *groth16_bls12377.Proof `json:"proof"`
}

type Ballot struct {
	ProcessID        types.HexBytes                                        `json:"processId"`
	VoterWeight      types.HexBytes                                        `json:"voterWeight"`
	EncryptedBallot  elgamal.Ballot                                        `json:"encryptedBallot"`
	Nullifier        types.HexBytes                                        `json:"nullifier"`
	Commitment       types.HexBytes                                        `json:"commitment"`
	Address          types.HexBytes                                        `json:"address"`
	BallotInputsHash types.HexBytes                                        `json:"ballotInputsHash"`
	BallotProof      recursion.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine] `json:"ballotProof"`
	Signature        types.BallotSignature                                 `json:"signature"`
	CensusProof      types.CensusProof                                     `json:"censusProof"`
	PubKey           types.HexBytes                                        `json:"publicKey"`
}

// Valid method checks if the Ballot is valid. A ballot is valid if all its
// components are valid. The BallotProof is not checked because it is a struct
// that comes from a third-party library (gnark) and it should be checked by
// the library itself.
func (b *Ballot) Valid() bool {
	return b.ProcessID != nil && b.VoterWeight != nil && b.Nullifier != nil &&
		b.Commitment != nil && b.Address != nil && b.PubKey != nil &&
		b.BallotInputsHash != nil && b.EncryptedBallot.Valid() &&
		b.Signature.Valid() && b.CensusProof.Valid()
}

type AggregatorBallotBatch struct {
	ProcessID types.HexBytes        `json:"processId"`
	Proof     *groth16_bw6761.Proof `json:"proof"`
	Ballots   []*AggregatorBallot   `json:"ballots"`
}

type AggregatorBallot struct {
	Nullifier       *big.Int       `json:"nullifiers"`
	Commitment      *big.Int       `json:"commitments"`
	Address         *big.Int       `json:"address"`
	EncryptedBallot elgamal.Ballot `json:"encryptedBallot"`
}
