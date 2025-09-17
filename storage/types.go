package storage

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	groth16_bls12377 "github.com/consensys/gnark/backend/groth16/bls12-377"
	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	groth16_bw6761 "github.com/consensys/gnark/backend/groth16/bw6-761"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"

	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
	recursion "github.com/consensys/gnark/std/recursion/groth16"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/crypto/signatures/ethereum"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// Process is the struct that contains the information of a process. It includes
// the census root, the ballot mode, the metadata hash and the encryption key.
// The census root is the root of the Merkle tree that contains the voters.
// The ballot mode contains the parameters that define the ballot protocol for
// the process. The metadata hash is the hash of the metadata of the process.
// The encryption key is the public key used to encrypt the ballots.
type Process struct {
	CensusRoot    types.HexBytes   `json:"censusRoot"`
	BallotMode    types.BallotMode `json:"ballotMode"`
	MetadataHash  types.HexBytes   `json:"metadataID"`
	EncryptionKey EncryptionKeys   `json:"encryptionKey"`
}

// EncryptionKeys is the struct that contains the public key used to encrypt
// the ballots. The public key is a point on the elliptic curve. It also
// contains the private key, but it is not exported in the JSON.
type EncryptionKeys struct {
	X          *big.Int `json:"publicKeyX" cbor:"0,keyasint,omitempty"`
	Y          *big.Int `json:"publicKeyY" cbor:"1,keyasint,omitempty"`
	PrivateKey *big.Int `json:"-" cbor:"2,keyasint,omitempty"`
}

// VerifiedBallot is the struct that contains the information of a ballot which
// has been verified by the sequencer. It includes the process ID, the voter
// weight, the encrypted ballot, the address, the inputs hash of the proof and
// the proof itself. The proof should be in the BLS12-377 curve, which is the
// one used by the verifier circuit and verified by the aggregator circuit.
type VerifiedBallot struct {
	VoteID          types.HexBytes          `json:"voteId"`
	ProcessID       types.HexBytes          `json:"processId"`
	VoterWeight     *big.Int                `json:"voterWeight"`
	EncryptedBallot *elgamal.Ballot         `json:"encryptedBallot"`
	Address         *big.Int                `json:"address"`
	InputsHash      *big.Int                `json:"inputsHash"`
	Proof           *groth16_bls12377.Proof `json:"proof"`
}

// Ballot is the struct that contains the information of a ballot. It includes
// the process ID, the voter weight, the encrypted ballot, the address, the
// inputs hash of the proof and the proof itself. The proof should be in the
// BN254 curve and ready for recursive verification. It also includes the
// signature of the ballot, which is a ECDSA signature. Finally, it includes
// the census proof, which proves that the voter is in the census; and the
// public key of the voter, a compressed ECDSA public key.
type Ballot struct {
	ProcessID        types.HexBytes                                        `json:"processId"`
	VoterWeight      *big.Int                                              `json:"voterWeight"`
	EncryptedBallot  *elgamal.Ballot                                       `json:"encryptedBallot"`
	Address          *big.Int                                              `json:"address"`
	BallotInputsHash *big.Int                                              `json:"ballotInputsHash"`
	BallotProof      recursion.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine] `json:"ballotProof"`
	Signature        *ethereum.ECDSASignature                              `json:"signature"`
	CensusProof      *types.CensusProof                                    `json:"censusProof"`
	PubKey           types.HexBytes                                        `json:"publicKey"`
	VoteID           types.HexBytes                                        `json:"voteId"`
}

// Valid method checks if the Ballot is valid. A ballot is valid if all its
// components are valid. The BallotProof is not checked because it is a struct
// that comes from a third-party library (gnark) and it should be checked by
// the library itself.
func (b *Ballot) Valid() bool {
	if b.ProcessID == nil || b.VoterWeight == nil || b.Address == nil ||
		b.BallotInputsHash == nil || b.EncryptedBallot == nil ||
		b.Signature == nil || b.CensusProof == nil || b.PubKey == nil {
		log.Debug("ballot is not valid, nil fields")
		return false
	}
	if !b.EncryptedBallot.Valid() {
		log.Debugf("encrypted ballot is not valid: %s", b.EncryptedBallot.String())
		return false
	}
	if !b.Signature.Valid() {
		log.Debugf("signature is not valid: %s", b.Signature.String())
		return false
	}
	if !b.CensusProof.Valid() {
		log.Debug("census proof is not valid")
		return false
	}
	return true
}

func (b *Ballot) String() string {
	s := strings.Builder{}
	s.WriteString("Ballot{")
	if b.ProcessID != nil {
		s.WriteString("ProcessID: " + b.ProcessID.String() + ", ")
	}
	if b.VoterWeight != nil {
		s.WriteString("VoterWeight: " + b.VoterWeight.String() + ", ")
	}
	if b.Address != nil {
		s.WriteString("Address: " + b.Address.String() + ", ")
	}
	if b.BallotInputsHash != nil {
		s.WriteString("BallotInputsHash: " + b.BallotInputsHash.String() + ", ")
	}
	if b.EncryptedBallot != nil {
		s.WriteString("EncryptedBallot: " + b.EncryptedBallot.String() + ", ")
	}
	if b.Signature != nil {
		s.WriteString("Signature: " + b.Signature.String() + ", ")
	}
	if b.CensusProof != nil {
		s.WriteString("CensusProof: " + b.CensusProof.String() + ", ")
	}
	if b.PubKey != nil {
		s.WriteString("PubKey: " + b.PubKey.String() + ", ")
	}
	s.WriteString("}")
	return s.String()
}

// AggregatorBallot is the struct that contains the information of a ballot
// which has been verified and aggregated in a batch by the sequencer. It
// includes the address and the encrypted ballot.
type AggregatorBallot struct {
	VoteID          types.HexBytes  `json:"voteId"`
	Address         *big.Int        `json:"address"`
	EncryptedBallot *elgamal.Ballot `json:"encryptedBallot"`
}

// AggregatorBallotBatch is the struct that contains the information of a
// batch of ballots which have been verified and aggregated by the sequencer.
// It includes the process ID, the proof of the batch and the ballots. The
// proof should be in the BW6-761 curve, which is the one used by the
// aggregator circuit and verified by the statetransition circuit.
type AggregatorBallotBatch struct {
	ProcessID types.HexBytes        `json:"processId"`
	Proof     *groth16_bw6761.Proof `json:"proof"`
	Ballots   []*AggregatorBallot   `json:"ballots"`
}

// StateTransitionBatch is the struct that contains the information of a
// transition of the state after include a batch of ballots. It includes the
// process ID, the proof of the batch and the ballots. The proof should be
// in the BN254 curve, which is the one used to verify the transition by the
// smart contract.
type StateTransitionBatch struct {
	ProcessID types.HexBytes                  `json:"processId"`
	Proof     *groth16_bn254.Proof            `json:"proof"`
	Ballots   []*AggregatorBallot             `json:"ballots"`
	Inputs    StateTransitionBatchProofInputs `json:"inputs"`

	BlobVersionHash common.Hash              `json:"blobVersionHash"`
	BlobSidecar     *gethtypes.BlobTxSidecar `json:"blobSidecar"`
}

// StateTransitionBatchProofInputs is the struct that contains the inputs
// of the proof of the state transition batch. It includes the root hash
// before and after the transition, the number of new votes and the number
// of overwrites.
type StateTransitionBatchProofInputs struct {
	RootHashBefore       *big.Int           `json:"rootHashBefore"`
	RootHashAfter        *big.Int           `json:"rootHashAfter"`
	NumNewVotes          int                `json:"numNewVotes"`
	NumOverwritten       int                `json:"numOverwritten"`
	BlobEvaluationPointZ *big.Int           `json:"blobEvaluationPointZ"`
	BlobEvaluationPointY [4]*big.Int        `json:"blobEvaluationPointY"`
	BlobComitment        kzg4844.Commitment `json:"blobCommitment"`
	BlobProof            kzg4844.Proof      `json:"blobProof"`
}

// ABIEncode packs the four fields as a single static uint256[4] blob:
//
//	[ rootHashBefore, rootHashAfter, numNewVotes, numOverwritten ]
func (s *StateTransitionBatchProofInputs) ABIEncode() ([]byte, error) {
	arr := [9]*big.Int{
		s.RootHashBefore,
		s.RootHashAfter,
		big.NewInt(int64(s.NumNewVotes)),
		big.NewInt(int64(s.NumOverwritten)),
		s.BlobEvaluationPointZ,    // Z is on bn254, so we don't need limbs
		s.BlobEvaluationPointY[0], // Y is on bls12-381, so we need all 4 limbs
		s.BlobEvaluationPointY[1],
		s.BlobEvaluationPointY[2],
		s.BlobEvaluationPointY[3],
	}
	arrType, err := abi.NewType("uint256[9]", "", nil)
	if err != nil {
		return nil, err
	}
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, err
	}
	arguments := abi.Arguments{
		{Type: arrType},
		{Type: bytesType}, // blobCommitment
		{Type: bytesType}, // blobProof
	}
	return arguments.Pack(arr, s.BlobComitment[:], s.BlobProof[:])
}

// String returns a JSON representation of the StateTransitionBatchProofInputs
// as a string. This is useful for debugging or logging purposes. If marshalling
// fails, it returns an empty JSON object as a string.
func (s *StateTransitionBatchProofInputs) String() string {
	jsonInputs, err := json.Marshal(s)
	if err != nil {
		return "{}" // Return empty JSON if marshalling fails
	}
	return string(jsonInputs)
}

// VerifiedResults is the struct that contains the information of a results
// of a process which has been verified by the sequencer. It includes the
// process ID, the proof of the results and the inputs of the proof.
type VerifiedResults struct {
	ProcessID types.HexBytes             `json:"processId"`
	Proof     *groth16_bn254.Proof       `json:"proof"`
	Inputs    ResultsVerifierProofInputs `json:"inputs"`
}

// ResultsVerifierProofInputs is the struct that contains the inputs of the
// results verifier proof. It includes the state root and the decrypted results
// of the votes.
type ResultsVerifierProofInputs struct {
	StateRoot *big.Int                        `json:"stateRoot"`
	Results   [types.FieldsPerBallot]*big.Int `json:"results"`
}

// ABIEncode packs the state root and results as a single static uint256[1 + N]
// blob, where N is the number of fields in the ballot (i.e., 4).
// The first element is the state root, followed by the results for each field.
//
//	[ stateRoot, result1, result2, ..., resultN ]
func (r *ResultsVerifierProofInputs) ABIEncode() ([]byte, error) {
	arr := append([]*big.Int{r.StateRoot}, r.Results[:]...)
	arrType, err := abi.NewType(fmt.Sprintf("uint256[%d]", len(arr)), "", nil)
	if err != nil {
		return nil, err
	}
	arguments := abi.Arguments{
		{Type: arrType},
	}
	return arguments.Pack(arr)
}

// String returns a JSON representation of the ResultsVerifierProofInputs
// as a string. This is useful for debugging or logging purposes. If
// marshalling fails, it returns an empty JSON object as a string.
func (s *ResultsVerifierProofInputs) String() string {
	jsonInputs, err := json.Marshal(s)
	if err != nil {
		return "{}" // Return empty JSON if marshalling fails
	}
	return string(jsonInputs)
}

// ProcessStatsUpdate represents a single stats update operation
type ProcessStatsUpdate struct {
	TypeStats types.TypeStats
	Delta     int
}

// Stats holds statistics for all processes.
type Stats struct {
	VerifiedVotesCount          int       `json:"verifiedVotes" cbor:"0,keyasint,omitempty"`
	AggregatedVotesCount        int       `json:"aggregatedVotes" cbor:"2,keyasint,omitempty"`
	StateTransitionCount        int       `json:"stateTransitions" cbor:"3,keyasint,omitempty"`
	SettledStateTransitionCount int       `json:"settledStateTransitions" cbor:"4,keyasint,omitempty"`
	LastStateTransitionDate     time.Time `json:"lastStateTransitionDate" cbor:"5,keyasint,omitempty"`
}

// StatsPendingBallots holds the total number of pending ballots and the last
// update time.
type StatsPendingBallots struct {
	TotalPendingBallots int       `json:"totalPendingBallots" cbor:"0,keyasint,omitempty"`
	LastUpdateDate      time.Time `json:"lastUpdateDate" cbor:"1,keyasint,omitempty"`
}

// MetadataHash returns the hash of the metadata.
func MetadataHash(metadata *types.Metadata) []byte {
	data, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return ethereum.HashRaw(data)
}
