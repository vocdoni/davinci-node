package state

import (
	"fmt"
	"math/big"
	"slices"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/circuits"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

var (
	// HashFn is the hash function used in the state tree.
	HashFn = arbo.HashFunctionMultiPoseidon
	// Curve is the curve used for the encryption
	Curve = curves.New(bjj.CurveType)
)

var (
	KeyProcessID     = new(big.Int).SetBytes([]byte{0x00})
	KeyCensusRoot    = new(big.Int).SetBytes([]byte{0x01})
	KeyBallotMode    = new(big.Int).SetBytes([]byte{0x02})
	KeyEncryptionKey = new(big.Int).SetBytes([]byte{0x03})
	KeyResultsAdd    = new(big.Int).SetBytes([]byte{0x04})
	KeyResultsSub    = new(big.Int).SetBytes([]byte{0x05})

	ErrStateAlreadyInitialized = fmt.Errorf("state already initialized")
)

// State represents a state tree
type State struct {
	tree      *arbo.Tree
	processID *big.Int
	db        db.Database
	dbTx      db.WriteTx

	oldResultsAdd      *elgamal.Ballot
	oldResultsSub      *elgamal.Ballot
	newResultsAdd      *elgamal.Ballot
	newResultsSub      *elgamal.Ballot
	ballotSum          *elgamal.Ballot
	overwrittenSum     *elgamal.Ballot
	overwrittenBallots []*elgamal.Ballot
	ballotCount        int
	overwrittenCount   int
	votes              []*Vote

	// Transition Witness
	rootHashBefore *big.Int
	processProofs  ProcessProofs
	votesProofs    VotesProofs
}

// ProcessProofs stores the Merkle proofs for the process, including the ID
// census root, ballot mode, and encryption key proofs.
type ProcessProofs struct {
	ID            *ArboProof
	CensusRoot    *ArboProof
	BallotMode    *ArboProof
	EncryptionKey *ArboProof
}

// VotesProofs stores the Merkle proofs for the votes, including the results
// add and sub proofs, as well as the ballot and commitment proofs.
type VotesProofs struct {
	ResultsAdd *ArboTransition
	ResultsSub *ArboTransition
	Ballot     [types.VotesPerBatch]*ArboTransition
}

// New creates or opens a State stored in the passed database.
// The processId is used as a prefix for the keys in the database.
func New(db db.Database, processId *big.Int) (*State, error) {
	pdb := prefixeddb.NewPrefixedDatabase(db, processId.Bytes())
	tree, err := arbo.NewTree(arbo.Config{
		Database:     pdb,
		MaxLevels:    types.StateTreeMaxLevels,
		HashFunction: HashFn,
	})
	if err != nil {
		return nil, err
	}
	return &State{
		db:        pdb,
		tree:      tree,
		processID: processId,
	}, nil
}

// LoadOnRoot loads a State from the database using the provided processId and
// root. It creates a new State with the given processId and sets the root of
// the tree to the provided root. It returns an error if the processId is not
// found in the database or if the root cannot be set.
// The root provided is formatted to the arbo format before being set in the
// state tree.
func LoadOnRoot(db db.Database, processId, root *big.Int) (*State, error) {
	state, err := New(db, processId)
	if err != nil {
		return nil, err
	}
	if err := state.SetRootAsBigInt(root); err != nil {
		return nil, err
	}
	return state, nil
}

// Initialize creates a new State, initialized with the passed parameters.
// After Initialize, caller is expected to StartBatch, AddVote, EndBatch,
// StartBatch...
func (o *State) Initialize(
	censusRoot *big.Int,
	ballotMode circuits.BallotMode[*big.Int],
	encryptionKey circuits.EncryptionKey[*big.Int],
) error {
	// Check if the state is already initialized
	if _, _, err := o.tree.GetBigInt(KeyProcessID); err == nil {
		return ErrStateAlreadyInitialized
	}
	if err := o.tree.AddBigInt(KeyProcessID, o.processID); err != nil {
		return err
	}
	if err := o.tree.AddBigInt(KeyCensusRoot, censusRoot); err != nil {
		return err
	}
	if err := o.tree.AddBigInt(KeyBallotMode, ballotMode.Serialize()...); err != nil {
		return err
	}
	if err := o.tree.AddBigInt(KeyEncryptionKey, encryptionKey.Serialize()...); err != nil {
		return err
	}
	if err := o.tree.AddBigInt(KeyResultsAdd, elgamal.NewBallot(Curve).BigInts()...); err != nil {
		return err
	}
	if err := o.tree.AddBigInt(KeyResultsSub, elgamal.NewBallot(Curve).BigInts()...); err != nil {
		return err
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

// StartBatch resets counters and sums to zero,
// and creates a new write transaction in the db
func (o *State) StartBatch() error {
	o.dbTx = o.db.WriteTx()
	o.oldResultsAdd = elgamal.NewBallot(Curve)
	o.oldResultsSub = elgamal.NewBallot(Curve)
	o.newResultsAdd = elgamal.NewBallot(Curve)
	o.newResultsSub = elgamal.NewBallot(Curve)
	o.ballotSum = elgamal.NewBallot(Curve)
	o.overwrittenSum = elgamal.NewBallot(Curve)
	o.ballotCount = 0
	o.overwrittenCount = 0
	o.overwrittenBallots = []*elgamal.Ballot{}
	o.votes = []*Vote{}
	return nil
}

// EndBatch commits the current batch to the database and generates the Merkle
// proofs for the current batch. It also updates the results of the state tree
// with the new results. The results are calculated by adding the old results
// with the new results. The function returns an error if the commit fails or
// if the Merkle proofs cannot be generated.
func (o *State) EndBatch() error {
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
	if o.processProofs.CensusRoot, err = o.GenArboProof(KeyCensusRoot); err != nil {
		return fmt.Errorf("could not get CensusRoot proof: %w", err)
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
		if i < len(o.Votes()) {
			o.votesProofs.Ballot[i], err = ArboTransitionFromAddOrUpdate(o,
				o.Votes()[i].Address, o.Votes()[i].Ballot.BigInts()...)
		} else {
			o.votesProofs.Ballot[i], err = ArboTransitionFromNoop(o)
		}
		if err != nil {
			return err
		}
	}
	// update ResultsAdd
	var ok bool
	o.oldResultsAdd, ok = o.ResultsAdd()
	if !ok {
		return fmt.Errorf("could not get old results add ballot")
	}
	o.newResultsAdd = o.newResultsAdd.Add(o.oldResultsAdd, o.ballotSum)
	o.votesProofs.ResultsAdd, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsAdd, o.newResultsAdd.BigInts()...)
	if err != nil {
		return fmt.Errorf("ResultsAdd: %w", err)
	}
	// update ResultsSub
	o.oldResultsSub, ok = o.ResultsSub()
	if !ok {
		return fmt.Errorf("could not get old results sub ballot")
	}
	o.newResultsSub = o.newResultsSub.Add(o.oldResultsSub, o.overwrittenSum)
	o.votesProofs.ResultsSub, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsSub, o.newResultsSub.BigInts()...)
	if err != nil {
		return fmt.Errorf("ResultsSub: %w", err)
	}
	return o.dbTx.Commit()
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

// BallotCount returns the number of ballots added in the current batch.
func (o *State) BallotCount() int {
	return o.ballotCount
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

// OverwrittenCount returns the number of ballots overwritten in the current
// batch.
func (o *State) OverwrittenCount() int {
	return o.overwrittenCount
}

// Votes returns the votes added in the current batch.
func (o *State) Votes() []*Vote {
	return o.votes
}

// OverwrittenBallots returns the overwritten ballots in the current batch.
func (o *State) OverwrittenBallots() []*elgamal.Ballot {
	v := slices.Clone(o.overwrittenBallots)
	for len(v) < types.VotesPerBatch {
		v = append(v, elgamal.NewBallot(Curve))
	}
	return v
}

// PaddedVotes returns the votes added in the current batch, padded to
// circuits.VotesPerBatch. The padding is done by adding empty votes with zero
// values.
func (o *State) PaddedVotes() []*Vote {
	v := slices.Clone(o.votes)
	log.Infof("Current batch has %d votes", len(v))
	for len(v) < types.VotesPerBatch {
		v = append(v, &Vote{
			Address: big.NewInt(0),
			Ballot:  elgamal.NewBallot(Curve),
		})
	}
	return v
}

// Proccess returns all process details from the state
func (o *State) Process() circuits.Process[*big.Int] {
	return circuits.Process[*big.Int]{
		ID:            o.ProcessID(),
		CensusRoot:    o.CensusRoot(),
		BallotMode:    o.BallotMode(),
		EncryptionKey: o.EncryptionKey(),
	}
}

// ProcessSerializeBigInts returns
//
//	process.ID
//	process.CensusRoot
//	process.BallotMode
//	process.EncryptionKey
func (o *State) ProcessSerializeBigInts() []*big.Int {
	list := []*big.Int{}
	list = append(list, o.ProcessID())
	list = append(list, o.CensusRoot())
	list = append(list, o.BallotMode().Serialize()...)
	list = append(list, o.EncryptionKey().Serialize()...)
	return list
}

// ProccessID returns the process ID of the state as a big.Int.
func (o *State) ProcessID() *big.Int {
	_, v, err := o.tree.GetBigInt(KeyProcessID)
	if err != nil {
		panic(err)
	}
	return v[0]
}

// CensusRoot returns the census root of the state as a big.Int.
func (o *State) CensusRoot() *big.Int {
	_, v, err := o.tree.GetBigInt(KeyCensusRoot)
	if err != nil {
		panic(err)
	}
	return v[0]
}

// BallotMode returns the ballot mode of the state as a
// circuits.BallotMode[*big.Int].
func (o *State) BallotMode() circuits.BallotMode[*big.Int] {
	_, v, err := o.tree.GetBigInt(KeyBallotMode)
	if err != nil {
		panic(err)
	}
	bm, err := new(circuits.BallotMode[*big.Int]).Deserialize(v)
	if err != nil {
		panic(err)
	}
	return bm
}

// EncryptionKey returns the encryption key of the state as a
// circuits.EncryptionKey[*big.Int].
func (o *State) EncryptionKey() circuits.EncryptionKey[*big.Int] {
	_, v, err := o.tree.GetBigInt(KeyEncryptionKey)
	if err != nil {
		panic(err)
	}
	ek, err := new(circuits.EncryptionKey[*big.Int]).Deserialize(v)
	if err != nil {
		panic(err)
	}
	return ek
}

// ResultsAdd returns the resultsAdd of the state as a elgamal.Ballot
func (o *State) ResultsAdd() (*elgamal.Ballot, bool) {
	_, v, err := o.tree.GetBigInt(KeyResultsAdd)
	if err != nil {
		return elgamal.NewBallot(Curve), false
	}
	resultsAdd, err := elgamal.NewBallot(Curve).SetBigInts(v)
	if err != nil {
		return elgamal.NewBallot(Curve), false
	}
	return resultsAdd, true
}

// SetResultsAdd sets the resultsAdd directly in the state tree
func (o *State) SetResultsAdd(resultsAdd *elgamal.Ballot) {
	if err := o.tree.UpdateBigInt(KeyResultsAdd, resultsAdd.BigInts()...); err != nil {
		panic(err)
	}
}

// SetResultsSub sets the resultsSub directly in the state tree
func (o *State) SetResultsSub(resultsSub *elgamal.Ballot) {
	if err := o.tree.UpdateBigInt(KeyResultsSub, resultsSub.BigInts()...); err != nil {
		panic(err)
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
	maxKeyLen := arbo.MaxKeyLen(types.StateTreeMaxLevels, HashFn.Len())
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
