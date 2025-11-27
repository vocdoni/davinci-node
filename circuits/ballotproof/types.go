package ballotproof

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/format"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/types"
)

// BallotProofInputs struct contains the required inputs to compose the
// data to generate the witness for a ballot proof using the circom circuit.
type BallotProofInputs struct {
	ProcessID     types.ProcessID   `json:"processId"`
	Address       types.HexBytes    `json:"address"`
	EncryptionKey []*types.BigInt   `json:"encryptionKey"`
	K             *types.BigInt     `json:"k"`
	BallotMode    *types.BallotMode `json:"ballotMode"`
	Weight        *types.BigInt     `json:"weight"`
	FieldValues   []*types.BigInt   `json:"fieldValues"`
}

// VoteID generates a unique identifier for the vote based on the process ID,
// address and k value. This ID is used to sign the vote and prove ownership.
// It returns the vote ID as a HexBytes type or an error if the inputs are
// invalid or something goes wrong during the generation of the ID. It calls
// the VoteID function with the process ID, address and k value converted to
// the appropriate types.
func (b *BallotProofInputs) VoteID() (*types.BigInt, error) {
	if b == nil {
		return nil, fmt.Errorf("ballot proof inputs cannot be nil")
	}
	return circuits.VoteID(b.ProcessID, common.BytesToAddress(b.Address), b.K)
}

// VoteIDForSign returns the vote ID in a format suitable for signing and
// verify the signature inside the circuit. It pads the vote ID to ensure it
// is of the correct length for signing.
func (b *BallotProofInputs) VoteIDForSign() (types.HexBytes, error) {
	voteID, err := b.VoteID()
	if err != nil {
		return nil, fmt.Errorf("error generating vote ID: %v", err.Error())
	}
	// return crypto.BigIntToFFToSign(voteID.MathBigInt(), circuits.VoteVerifierCurve.ScalarField()), nil
	return crypto.PadToSign(voteID.Bytes()), err
}

func (b *BallotProofInputs) Serialize(ballot *elgamal.Ballot) ([]*big.Int, error) {
	// safe address and processID
	ffAddress := b.Address.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	ffProcessID := b.ProcessID.BigInt().ToFF(circuits.BallotProofCurve.ScalarField())
	// convert the encryption key to twisted edwards form
	encryptionKey := new(bjj.BJJ).SetPoint(b.EncryptionKey[0].MathBigInt(), b.EncryptionKey[1].MathBigInt())
	encryptionKeyXTE, encryptionKeyYTE := format.FromRTEtoTE(encryptionKey.Point())
	// ballot mode as circuit ballot mode
	circuitBallotMode := circuits.BallotModeToCircuit(b.BallotMode)
	// vote ID
	voteID, err := b.VoteID()
	if err != nil {
		return nil, fmt.Errorf("error generating vote ID: %w", err)
	}
	// compose a list with the inputs of the circuit to hash them
	inputsHash := []*big.Int{ffProcessID.MathBigInt()}                // process id
	inputsHash = append(inputsHash, circuitBallotMode.Serialize()...) // ballot mode serialized
	inputsHash = append(inputsHash,
		encryptionKeyXTE,       // encryption key x coordinate
		encryptionKeyYTE,       // encryption key y coordinate
		ffAddress.MathBigInt(), // address
		voteID.MathBigInt(),    // vote ID
	)
	// ballot (in twisted edwards form)
	inputsHash = append(inputsHash, ballot.FromRTEtoTE().BigInts()...)
	// weight
	inputsHash = append(inputsHash, b.Weight.MathBigInt())
	return inputsHash, nil
}

// CircomInputs struct contains the data of the witness to generate a ballot
// proof using the circom circuit.
type CircomInputs struct {
	Fields         []*types.BigInt `json:"fields"`
	NumFields      *types.BigInt   `json:"num_fields"`
	UniqueValues   *types.BigInt   `json:"unique_values"`
	MaxValue       *types.BigInt   `json:"max_value"`
	MinValue       *types.BigInt   `json:"min_value"`
	MaxValueSum    *types.BigInt   `json:"max_value_sum"`
	MinValueSum    *types.BigInt   `json:"min_value_sum"`
	CostExponent   *types.BigInt   `json:"cost_exponent"`
	CostFromWeight *types.BigInt   `json:"cost_from_weight"`
	Address        *types.BigInt   `json:"address"`
	Weight         *types.BigInt   `json:"weight"`
	ProcessID      *types.BigInt   `json:"process_id"`
	VoteID         *types.BigInt   `json:"vote_id"`
	EncryptionKey  []*types.BigInt `json:"encryption_pubkey"`
	K              *types.BigInt   `json:"k"`
	Cipherfields   []*types.BigInt `json:"cipherfields"`
	InputsHash     *types.BigInt   `json:"inputs_hash"`
}

// BallotProofInputsResult struct contains the result of composing the data to
// generate the witness for a ballot proof using the circom circuit. Includes
// the inputs for the circom circuit but also the required data to cast a vote
// sending it to the sequencer API. It includes the BallotInputsHash, which is
// used by the API to verify the resulting circom proof and the voteID, which
// is signed by the user to prove the ownership of the vote.
type BallotProofInputsResult struct {
	ProcessID        types.ProcessID `json:"processId"`
	Address          types.HexBytes  `json:"address"`
	Weight           *types.BigInt   `json:"weight"`
	Ballot           *elgamal.Ballot `json:"ballot"`
	BallotInputsHash *types.BigInt   `json:"ballotInputsHash"`
	VoteID           types.HexBytes  `json:"voteId"`
	CircomInputs     *CircomInputs   `json:"circomInputs"`
}
