package state

import (
	"fmt"
	"math/big"
	"slices"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

var (
	// HashFn is the hash function used in the state tree.
	HashFn = arbo.HashFunctionMultiPoseidon
	// Curve is the curve used for the encryption
	Curve = curves.New(bjj.CurveType)
)

var (
	KeyProcessID     = big.NewInt(circuits.KeyProcessID)
	KeyBallotMode    = big.NewInt(circuits.KeyBallotMode)
	KeyEncryptionKey = big.NewInt(circuits.KeyEncryptionKey)
	KeyResultsAdd    = big.NewInt(circuits.KeyResultsAdd)
	KeyResultsSub    = big.NewInt(circuits.KeyResultsSub)
	KeyCensusOrigin  = big.NewInt(circuits.KeyCensusOrigin)

	ReservedKeysOffset = big.NewInt(circuits.ReservedKeysOffset)

	ErrStateAlreadyInitialized = fmt.Errorf("state already initialized")
)

// State represents a state tree
type State struct {
	tree      *arbo.Tree
	processID types.ProcessID
	db        db.Database
	dbTx      db.WriteTx

	oldResultsAdd         *elgamal.Ballot
	oldResultsSub         *elgamal.Ballot
	newResultsAdd         *elgamal.Ballot
	newResultsSub         *elgamal.Ballot
	allBallotsSum         *elgamal.Ballot
	overwrittenSum        *elgamal.Ballot
	votersCount           int
	overwrittenVotesCount int
	votes                 []*Vote

	// Transition Witness
	rootHashBefore *big.Int
	processProofs  ProcessProofs
	votesProofs    VotesProofs
}

// ProcessProofs stores the Merkle proofs for the process, including the ID
// census root, ballot mode, and encryption key proofs.
type ProcessProofs struct {
	ID            *ArboProof
	CensusOrigin  *ArboProof
	BallotMode    *ArboProof
	EncryptionKey *ArboProof
}

// VotesProofs stores the Merkle proofs for the votes, including the results
// add and sub proofs, as well as the ballot and commitment proofs.
type VotesProofs struct {
	ResultsAdd *ArboTransition
	ResultsSub *ArboTransition
	Ballot     [params.VotesPerBatch]*ArboTransition
	VoteID     [params.VotesPerBatch]*ArboTransition
}

// New creates or opens a State stored in the passed database.
// The processID is used as a prefix for the keys in the database.
func New(db db.Database, processID types.ProcessID) (*State, error) {
	// the process ID must be in the scalar field of the circuit curve
	processID.ToFF(params.StateTransitionCurve.ScalarField())

	if !processID.IsValid() {
		return nil, fmt.Errorf("processID is not valid")
	}

	pdb := prefixeddb.NewPrefixedDatabase(db, processID.Bytes())
	tree, err := arbo.NewTree(arbo.Config{
		Database:     pdb,
		MaxLevels:    params.StateTreeMaxLevels,
		HashFunction: HashFn,
	})
	if err != nil {
		return nil, err
	}
	return &State{
		db:        pdb,
		tree:      tree,
		processID: processID,
	}, nil
}

// LoadOnRoot loads a State from the database using the provided processId and
// root. It creates a new State with the given processId and sets the root of
// the tree to the provided root. It returns an error if the processId is not
// found in the database or if the root cannot be set.
// The root provided is formatted to the arbo format before being set in the
// state tree.
func LoadOnRoot(db db.Database, processId types.ProcessID, root *big.Int) (*State, error) {
	state, err := New(db, processId)
	if err != nil {
		return nil, fmt.Errorf("could not open state: %v", err)
	}
	if err := state.SetRootAsBigInt(root); err != nil {
		return nil, fmt.Errorf("could not set state root: %v", err)
	}
	return state, nil
}

// RootExists checks if the provided root exists in the tree for the given
// processId. Returns nil if the root exists, or an error if it does not.
func RootExists(db db.Database, processId types.ProcessID, root *big.Int) error {
	state, err := New(db, processId)
	if err != nil {
		return fmt.Errorf("could not open state: %v", err)
	}
	if err := state.RootExists(root); err != nil {
		return fmt.Errorf("could not find root in state: %v", err)
	}
	return nil
}

// CalculateInitialRoot returns the root of the tree that would result from
// initializing a state with the passed parameters. It uses an ephemereal
// tree, nothing is written down to storage.
func CalculateInitialRoot(
	processID types.ProcessID,
	censusOrigin *big.Int,
	ballotMode *types.BallotMode,
	publicKey ecc.Point,
) (*big.Int, error) {
	// Initialize the state in a memDB, just to calculate stateRoot
	st, err := New(memdb.New(), processID)
	if err != nil {
		return nil, fmt.Errorf("could not create state: %v", err)
	}

	defer func() {
		if err := st.Close(); err != nil {
			log.Warnw("failed to close state", "error", err)
		}
	}()

	// Initialize the state with the census root, ballot mode and the encryption key
	if err := st.Initialize(
		censusOrigin,
		circuits.BallotModeToCircuit(ballotMode),
		circuits.EncryptionKeyFromECCPoint(publicKey)); err != nil {
		return nil, fmt.Errorf("could not initialize state: %v", err)
	}

	return st.RootAsBigInt()
}

// Initialize creates a new State, initialized with the passed parameters.
// After Initialize, caller is expected to StartBatch, AddVote, EndBatch,
// StartBatch...
func (o *State) Initialize(
	censusOrigin *big.Int,
	ballotMode circuits.BallotMode[*big.Int],
	encryptionKey circuits.EncryptionKey[*big.Int],
) error {
	// Check if the state is already initialized
	if _, _, err := o.tree.GetBigInt(KeyProcessID); err == nil {
		return ErrStateAlreadyInitialized
	}
	if err := o.tree.AddBigInt(KeyProcessID, o.processID.MathBigInt()); err != nil {
		return fmt.Errorf("could not set process ID: %w", err)
	}
	if err := o.tree.AddBigInt(KeyBallotMode, ballotMode.Serialize()...); err != nil {
		return fmt.Errorf("could not set ballot mode: %w", err)
	}
	if err := o.tree.AddBigInt(KeyEncryptionKey, encryptionKey.Serialize()...); err != nil {
		return fmt.Errorf("could not set encryption key: %w", err)
	}
	if err := o.tree.AddBigInt(KeyResultsAdd, elgamal.NewBallot(Curve).BigInts()...); err != nil {
		return fmt.Errorf("could not set results add: %w", err)
	}
	if err := o.tree.AddBigInt(KeyResultsSub, elgamal.NewBallot(Curve).BigInts()...); err != nil {
		return fmt.Errorf("could not set results sub: %w", err)
	}
	if err := o.tree.AddBigInt(KeyCensusOrigin, censusOrigin); err != nil {
		return fmt.Errorf("could not set census origin: %w", err)
	}
	return nil
}

// Close the database, no more operations can be done after this.
func (o *State) Close() error {
	if o.dbTx != nil {
		o.dbTx.Discard()
	}
	return nil
}

func (o *State) AddVotesBatch(votes []*Vote) error {
	if err := o.startBatch(); err != nil {
		return fmt.Errorf("failed to start batch: %w", err)
	}
	for _, v := range votes {
		if err := o.addVote(v); err != nil {
			return fmt.Errorf("failed to add vote: %w", err)
		}
	}
	if err := o.endBatch(); err != nil {
		return fmt.Errorf("failed to end batch: %w", err)
	}
	return nil
}

// StartBatch resets counters and sums to zero,
// and creates a new write transaction in the db
func (o *State) startBatch() error {
	o.dbTx = o.db.WriteTx()
	o.oldResultsAdd = elgamal.NewBallot(Curve)
	o.oldResultsSub = elgamal.NewBallot(Curve)
	o.newResultsAdd = elgamal.NewBallot(Curve)
	o.newResultsSub = elgamal.NewBallot(Curve)
	o.allBallotsSum = elgamal.NewBallot(Curve)
	o.overwrittenSum = elgamal.NewBallot(Curve)
	o.votersCount = 0
	o.overwrittenVotesCount = 0
	o.votes = []*Vote{}
	return nil
}

// EndBatch commits the current batch to the database and generates the Merkle
// proofs for the current batch. It also updates the results of the state tree
// with the new results. The results are calculated by adding the old results
// with the new results. The function returns an error if the commit fails or
// if the Merkle proofs cannot be generated.
func (o *State) endBatch() error {
	var err error
	// RootHashBefore
	o.rootHashBefore, err = o.RootAsBigInt()
	if err != nil {
		return err
	}

	// first get MerkleProofs, since they need to belong to RootHashBefore, i.e.
	// before MerkleTransitions
	if o.processProofs.ID, err = o.GenArboProof(KeyProcessID); err != nil {
		return fmt.Errorf("could not get ID proof: %w", err)
	}
	if o.processProofs.CensusOrigin, err = o.GenArboProof(KeyCensusOrigin); err != nil {
		return fmt.Errorf("could not get CensusOrigin proof: %w", err)
	}
	if o.processProofs.BallotMode, err = o.GenArboProof(KeyBallotMode); err != nil {
		return fmt.Errorf("could not get BallotMode proof: %w", err)
	}
	if o.processProofs.EncryptionKey, err = o.GenArboProof(KeyEncryptionKey); err != nil {
		return fmt.Errorf("could not get EncryptionKey proof: %w", err)
	}

	// now build ordered chain of MerkleTransitions. The order should be the
	// same that the circuit will process them, so that the MerkleProofs are
	// in the same order as the MerkleTransitions

	// add Ballots
	for i := range o.votesProofs.Ballot {
		var errBallot, errVoteID error
		if i < len(o.Votes()) {
			o.votesProofs.Ballot[i], errBallot = ArboTransitionFromAddOrUpdate(o,
				o.Votes()[i].Address, o.Votes()[i].ReencryptedBallot.BigInts()...)
			o.votesProofs.VoteID[i], errVoteID = ArboTransitionFromAddOrUpdate(o,
				o.Votes()[i].VoteID.BigInt().MathBigInt(), VoteIDKeyValue)
		} else {
			o.votesProofs.Ballot[i], errBallot = ArboTransitionFromNoop(o)
			o.votesProofs.VoteID[i], errVoteID = ArboTransitionFromNoop(o)
		}
		if errBallot != nil {
			return fmt.Errorf("could not get Ballot proof for index %d: %w", i, errBallot)
		}
		if errVoteID != nil {
			return fmt.Errorf("could not get VoteID proof for index %d: %w", i, errVoteID)
		}
	}
	// update ResultsAdd
	var ok bool
	o.oldResultsAdd, ok = o.ResultsAdd()
	if !ok {
		return fmt.Errorf("resultsAdd not found in state")
	}
	o.newResultsAdd = o.newResultsAdd.Add(o.oldResultsAdd, o.allBallotsSum)
	o.votesProofs.ResultsAdd, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsAdd, o.newResultsAdd.BigInts()...)
	if err != nil {
		return fmt.Errorf("resultsAdd: %w", err)
	}

	// update ResultsSub
	o.oldResultsSub, ok = o.ResultsSub()
	if !ok {
		return fmt.Errorf("resultsSub not found in state")
	}
	o.newResultsSub = o.newResultsSub.Add(o.oldResultsSub, o.overwrittenSum)
	o.votesProofs.ResultsSub, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsSub, o.newResultsSub.BigInts()...)
	if err != nil {
		return fmt.Errorf("resultsSub: %w", err)
	}

	// Commit the transaction
	if err := o.dbTx.Commit(); err != nil {
		return err
	}

	return nil
}

// Root method returns the root of the tree as a byte array.
func (o *State) Root() ([]byte, error) {
	return o.tree.Root()
}

// RootAsBigInt method returns the root of the tree as a big.Int.
func (o *State) RootAsBigInt() (*big.Int, error) {
	root, err := o.tree.Root()
	if err != nil {
		return nil, err
	}
	return BytesToBigInt(root), nil
}

// SetRoot method sets the root of the tree to the provided one.
func (o *State) SetRoot(newRoot []byte) error {
	if err := o.tree.SetRoot(newRoot); err != nil {
		return err
	}
	return nil
}

// SetRootAsBigInt method sets the root of the tree to the provided one as a
// big.Int.
func (o *State) SetRootAsBigInt(newRoot *big.Int) error {
	if err := o.tree.SetRoot(BigIntToBytes(newRoot)); err != nil {
		return err
	}
	return nil
}

// RootExists checks if the provided root exists in the tree.
// Returns nil if the root exists, or an error if it does not.
func (o *State) RootExists(root *big.Int) error {
	return o.tree.RootExists(BigIntToBytes(root))
}

// VotersCount returns the number of voters participating in the current batch,
// i.e. either casting their first vote or overwriting a previous one.
func (o *State) VotersCount() int {
	return o.votersCount
}

// OldResultsAdd returns the old results add ballot of the current batch.
func (o *State) OldResultsAdd() *elgamal.Ballot {
	return o.oldResultsAdd
}

// OldResultsSub returns the old results sub ballot of the current batch.
func (o *State) OldResultsSub() *elgamal.Ballot {
	return o.oldResultsSub
}

// NewResultsAdd returns the new results add ballot of the current batch.
func (o *State) NewResultsAdd() *elgamal.Ballot {
	return o.newResultsAdd
}

// NewResultsSub returns the new results sub ballot of the current batch.
func (o *State) NewResultsSub() *elgamal.Ballot {
	return o.newResultsSub
}

// OverwrittenVotesCount returns the number of ballots overwritten in the current
// batch.
func (o *State) OverwrittenVotesCount() int {
	return o.overwrittenVotesCount
}

// Votes returns the votes added in the current batch.
func (o *State) Votes() []*Vote {
	return o.votes
}

// PaddedVotes returns the votes added in the current batch, padded to
// circuits.VotesPerBatch. The padding is done by adding empty votes with zero
// values.
func (o *State) PaddedVotes() []*Vote {
	v := slices.Clone(o.votes)
	for len(v) < params.VotesPerBatch {
		v = append(v, &Vote{
			Address:           big.NewInt(0),
			Ballot:            elgamal.NewBallot(Curve),
			ReencryptedBallot: elgamal.NewBallot(Curve),
			OverwrittenBallot: elgamal.NewBallot(Curve),
			Weight:            big.NewInt(0),
		})
	}
	return v
}

// Proccess returns all process details from the state
func (o *State) Process() circuits.Process[*big.Int] {
	return circuits.Process[*big.Int]{
		ID:            o.ProcessID(),
		CensusOrigin:  o.CensusOrigin(),
		BallotMode:    o.BallotMode(),
		EncryptionKey: o.EncryptionKey(),
	}
}

// ProcessSerializeBigInts returns
//
//	process.ID
//	process.CensusOrigin
//	process.BallotMode
//	process.EncryptionKey
func (o *State) ProcessSerializeBigInts() []*big.Int {
	list := []*big.Int{}
	list = append(list, o.ProcessID())
	list = append(list, o.CensusOrigin())
	list = append(list, o.BallotMode().Serialize()...)
	list = append(list, o.EncryptionKey().Serialize()...)
	return list
}

// ProccessID returns the process ID of the state as a big.Int.
func (o *State) ProcessID() *big.Int {
	_, v, err := o.tree.GetBigInt(KeyProcessID)
	if err != nil {
		log.Errorw(err, "failed to get process ID from state")
	}
	if len(v) == 0 {
		return big.NewInt(0) // default value if not set
	}
	return v[0]
}

// CensusOrigin returns the census origin of the state as a *big.Int.
func (o *State) CensusOrigin() *big.Int {
	_, v, err := o.tree.GetBigInt(KeyCensusOrigin)
	if err != nil {
		log.Errorw(err, "failed to get census origin from state")
	}
	if len(v) == 0 {
		return big.NewInt(0) // default value if not set
	}
	return v[0]
}

// BallotMode returns the ballot mode of the state as a
// circuits.BallotMode[*big.Int].
func (o *State) BallotMode() circuits.BallotMode[*big.Int] {
	_, v, err := o.tree.GetBigInt(KeyBallotMode)
	if err != nil {
		log.Errorw(err, "failed to get ballot mode from state")
	}
	bm, err := new(circuits.BallotMode[*big.Int]).Deserialize(v)
	if err != nil {
		log.Errorw(err, "failed to deserialize ballot mode in state")
	}
	return bm
}

// EncryptionKey returns the encryption key of the state as a
// circuits.EncryptionKey[*big.Int].
func (o *State) EncryptionKey() circuits.EncryptionKey[*big.Int] {
	_, v, err := o.tree.GetBigInt(KeyEncryptionKey)
	if err != nil {
		log.Errorw(err, "failed to get encryption key from state")
	}
	ek, err := new(circuits.EncryptionKey[*big.Int]).Deserialize(v)
	if err != nil {
		log.Errorw(err, "failed to deserialize encryption key in state")
	}
	return ek
}

// ResultsAdd returns the resultsAdd of the state as a elgamal.Ballot
func (o *State) ResultsAdd() (*elgamal.Ballot, bool) {
	_, v, err := o.tree.GetBigInt(KeyResultsAdd)
	if err != nil {
		log.Errorw(err, "failed to get resultsAdd from state")
		return elgamal.NewBallot(Curve), false
	}
	resultsAdd, err := elgamal.NewBallot(Curve).SetBigInts(v)
	if err != nil {
		log.Errorw(err, "failed to set resultsAdd from state")
		return elgamal.NewBallot(Curve), false
	}
	return resultsAdd, true
}

// SetResultsAdd sets the resultsAdd directly in the state tree
func (o *State) SetResultsAdd(resultsAdd *elgamal.Ballot) {
	if err := o.tree.UpdateBigInt(KeyResultsAdd, resultsAdd.BigInts()...); err != nil {
		log.Errorw(err, "failed to set resultsAdd in state")
	}
}

// SetResultsSub sets the resultsSub directly in the state tree
func (o *State) SetResultsSub(resultsSub *elgamal.Ballot) {
	if err := o.tree.UpdateBigInt(KeyResultsSub, resultsSub.BigInts()...); err != nil {
		log.Errorw(err, "failed to set resultsSub in state")
	}
}

// ResultsSub returns the resultsSub of the state as a elgamal.Ballot
func (o *State) ResultsSub() (*elgamal.Ballot, bool) {
	_, v, err := o.tree.GetBigInt(KeyResultsSub)
	if err != nil {
		return elgamal.NewBallot(Curve), false
	}
	resultsSub, err := elgamal.NewBallot(Curve).SetBigInts(v)
	if err != nil {
		return elgamal.NewBallot(Curve), false
	}
	return resultsSub, true
}

// EncodeKey encodes a key to a byte array using the maximum key length for the
// current number of levels in the state tree and the hash function length.
func EncodeKey(key *big.Int) []byte {
	maxKeyLen := arbo.MaxKeyLen(params.StateTreeMaxLevels, HashFn.Len())
	return arbo.BigIntToBytes(maxKeyLen, key)
}

// RootHashBefore returns the root hash before state transition.
func (o *State) RootHashBefore() *big.Int {
	return o.rootHashBefore
}

// ProcessProofs returns a pointer to the process proofs for the state.
func (o *State) ProcessProofs() ProcessProofs {
	return o.processProofs
}

// VotesProofs returns a pointer to the votes proofs for the state.
func (o *State) VotesProofs() VotesProofs {
	return o.votesProofs
}

// keyIsBelowReservedOffset returns true when passed key is below the ReservedKeysOffset
func keyIsBelowReservedOffset(key *big.Int) bool {
	return key.Cmp(ReservedKeysOffset) == -1
}
