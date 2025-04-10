package state

import (
	"fmt"
	"math/big"
	"slices"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

var (
	// HashFunc is the hash function used in the state tree.
	HashFunc = arbo.HashFunctionMiMC_BN254
	// Curve is the curve used for the encryption
	Curve = curves.New(bjj.CurveType)
)

var (
	KeyProcessID     = []byte{0x00}
	KeyCensusRoot    = []byte{0x01}
	KeyBallotMode    = []byte{0x02}
	KeyEncryptionKey = []byte{0x03}
	KeyResultsAdd    = []byte{0x04}
	KeyResultsSub    = []byte{0x05}
)

// State represents a state tree
type State struct {
	tree      *arbo.Tree
	processID []byte
	db        db.Database
	dbTx      db.WriteTx

	// TODO: unexport these, add ArboProofs and only export those via a method
	OldResultsAdd      *elgamal.Ballot
	OldResultsSub      *elgamal.Ballot
	NewResultsAdd      *elgamal.Ballot
	NewResultsSub      *elgamal.Ballot
	BallotSum          *elgamal.Ballot
	OverwriteSum       *elgamal.Ballot
	overwrittenBallots []*elgamal.Ballot
	ballotCount        int
	overwriteCount     int
	votes              []*Vote

	// Transition Witness
	RootHashBefore *big.Int
	Process        circuits.Process[*big.Int]
	ProcessProofs  ProcessProofs
	VotesProofs    VotesProofs
}
type ProcessProofs struct {
	ID            *ArboProof
	CensusRoot    *ArboProof
	BallotMode    *ArboProof
	EncryptionKey *ArboProof
}

type VotesProofs struct {
	ResultsAdd *ArboTransition
	ResultsSub *ArboTransition
	Ballot     [types.VotesPerBatch]*ArboTransition
	Commitment [types.VotesPerBatch]*ArboTransition
}

// New creates or opens a State stored in the passed database.
// The processId is used as a prefix for the keys in the database.
func New(db db.Database, processId []byte) (*State, error) {
	pdb := prefixeddb.NewPrefixedDatabase(db, processId)
	tree, err := arbo.NewTree(arbo.Config{
		Database: pdb, MaxLevels: types.CensusTreeMaxLevels,
		HashFunction: HashFunc,
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

// Initialize creates a new State, initialized with the passed parameters.
//
// after Initialize, caller is expected to StartBatch, AddVote, EndBatch, StartBatch...
func (o *State) Initialize(censusRoot, ballotMode, encryptionKey []byte) error {
	if err := o.tree.Add(KeyProcessID, o.processID); err != nil {
		return err
	}
	if err := o.tree.Add(KeyCensusRoot, censusRoot); err != nil {
		return err
	}
	if err := o.tree.Add(KeyBallotMode, ballotMode); err != nil {
		return err
	}
	if err := o.tree.Add(KeyEncryptionKey, encryptionKey); err != nil {
		return err
	}
	if err := o.tree.Add(KeyResultsAdd, elgamal.NewBallot(Curve).Serialize()); err != nil {
		return err
	}
	if err := o.tree.Add(KeyResultsSub, elgamal.NewBallot(Curve).Serialize()); err != nil {
		return err
	}

	o.Process.ID = arbo.BytesToBigInt(o.processID)
	o.Process.CensusRoot = arbo.BytesToBigInt(censusRoot)
	var err error
	o.Process.BallotMode, err = circuits.DeserializeBallotMode(ballotMode)
	if err != nil {
		return err
	}
	o.Process.EncryptionKey, err = circuits.DeserializeEncryptionKey(encryptionKey)
	if err != nil {
		return err
	}

	return nil
}

// Close the database, no more operations can be done after this.
func (o *State) Close() error {
	return o.db.Close()
}

// StartBatch resets counters and sums to zero,
// and creates a new write transaction in the db
func (o *State) StartBatch() error {
	o.dbTx = o.db.WriteTx()
	if o.OldResultsAdd == nil {
		o.OldResultsAdd = elgamal.NewBallot(Curve)
	}
	if o.OldResultsSub == nil {
		o.OldResultsSub = elgamal.NewBallot(Curve)
	}
	if o.NewResultsAdd == nil {
		o.NewResultsAdd = elgamal.NewBallot(Curve)
	}
	if o.NewResultsSub == nil {
		o.NewResultsSub = elgamal.NewBallot(Curve)
	}
	{
		_, v, err := o.tree.Get(KeyResultsAdd)
		if err != nil {
			return err
		}
		if err := o.OldResultsAdd.Deserialize(v); err != nil {
			return fmt.Errorf("OldResultsAdd: %w", err)
		}
	}
	{
		_, v, err := o.tree.Get(KeyResultsSub)
		if err != nil {
			return err
		}
		if err := o.OldResultsSub.Deserialize(v); err != nil {
			return fmt.Errorf("OldResultsSub: %w", err)
		}
	}

	o.BallotSum = elgamal.NewBallot(Curve)
	o.OverwriteSum = elgamal.NewBallot(Curve)
	o.ballotCount = 0
	o.overwriteCount = 0
	o.overwrittenBallots = []*elgamal.Ballot{}
	o.votes = []*Vote{}
	return nil
}

func (o *State) EndBatch() error {
	var err error
	// RootHashBefore
	o.RootHashBefore, err = o.RootAsBigInt()
	if err != nil {
		return err
	}

	// first get MerkleProofs, since they need to belong to RootHashBefore, i.e. before MerkleTransitions
	if o.ProcessProofs.ID, err = o.GenArboProof(KeyProcessID); err != nil {
		return err
	}
	if o.ProcessProofs.CensusRoot, err = o.GenArboProof(KeyCensusRoot); err != nil {
		return err
	}
	if o.ProcessProofs.BallotMode, err = o.GenArboProof(KeyBallotMode); err != nil {
		return err
	}
	if o.ProcessProofs.EncryptionKey, err = o.GenArboProof(KeyEncryptionKey); err != nil {
		return err
	}

	// now build ordered chain of MerkleTransitions

	// add Ballots
	for i := range o.VotesProofs.Ballot {
		if i < len(o.Votes()) {
			o.VotesProofs.Ballot[i], err = ArboTransitionFromAddOrUpdate(o,
				o.Votes()[i].Nullifier, o.Votes()[i].Ballot.Serialize())
		} else {
			o.VotesProofs.Ballot[i], err = ArboTransitionFromNoop(o)
		}
		if err != nil {
			return err
		}
	}

	// add Commitments
	for i := range o.VotesProofs.Commitment {
		if i < len(o.Votes()) {
			o.VotesProofs.Commitment[i], err = ArboTransitionFromAddOrUpdate(o,
				o.Votes()[i].Address, arbo.BigIntToBytes(crypto.SerializedFieldSize, o.Votes()[i].Commitment))
		} else {
			o.VotesProofs.Commitment[i], err = ArboTransitionFromNoop(o)
		}
		if err != nil {
			return err
		}
	}

	// update ResultsAdd
	o.NewResultsAdd = o.NewResultsAdd.Add(o.OldResultsAdd, o.BallotSum)
	o.VotesProofs.ResultsAdd, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsAdd, o.NewResultsAdd.Serialize())
	if err != nil {
		return fmt.Errorf("ResultsAdd: %w", err)
	}

	// update ResultsSub
	o.NewResultsSub = o.NewResultsSub.Add(o.OldResultsSub, o.OverwriteSum)
	o.VotesProofs.ResultsSub, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsSub, o.NewResultsSub.Serialize())
	if err != nil {
		return fmt.Errorf("ResultsSub: %w", err)
	}

	return o.dbTx.Commit()
}

func (o *State) Root() ([]byte, error) {
	return o.tree.Root()
}

func (o *State) RootAsBigInt() (*big.Int, error) {
	root, err := o.tree.Root()
	if err != nil {
		return nil, err
	}
	return arbo.BytesToBigInt(root), nil
}

func (o *State) BallotCount() int {
	return o.ballotCount
}

func (o *State) OverwriteCount() int {
	return o.overwriteCount
}

func (o *State) Votes() []*Vote {
	return o.votes
}

func (o *State) OverwrittenBallots() []*elgamal.Ballot {
	v := slices.Clone(o.overwrittenBallots)
	for len(v) < types.VotesPerBatch {
		v = append(v, elgamal.NewBallot(Curve))
	}
	return v
}

func (o *State) PaddedVotes() []*Vote {
	v := slices.Clone(o.votes)
	for len(v) < types.VotesPerBatch {
		v = append(v, &Vote{
			Address:    []byte{0x00},
			Commitment: big.NewInt(0),
			Nullifier:  []byte{0x00},
			Ballot:     elgamal.NewBallot(Curve),
		})
	}
	return v
}

// ProcessSerializeBigInts returns
//
//	process.ID
//	process.CensusRoot
//	process.BallotMode
//	process.EncryptionKey
func (o *State) ProcessSerializeBigInts() []*big.Int {
	list := []*big.Int{}
	list = append(list, arbo.BytesToBigInt(o.ProcessID()))
	list = append(list, arbo.BytesToBigInt(o.CensusRoot()))
	list = append(list, o.BallotMode().Serialize()...)
	list = append(list, o.EncryptionKey().Serialize()...)
	return list
}

func (o *State) ProcessID() []byte {
	_, v, err := o.tree.Get(KeyProcessID)
	if err != nil {
		panic(err)
	}
	return v
}

func (o *State) CensusRoot() []byte {
	_, v, err := o.tree.Get(KeyCensusRoot)
	if err != nil {
		panic(err)
	}
	return v
}

func (o *State) BallotMode() circuits.BallotMode[*big.Int] {
	_, v, err := o.tree.Get(KeyBallotMode)
	if err != nil {
		panic(err)
	}
	bm, err := circuits.DeserializeBallotMode(v)
	if err != nil {
		panic(err)
	}
	return bm
}

func (o *State) EncryptionKey() circuits.EncryptionKey[*big.Int] {
	_, v, err := o.tree.Get(KeyEncryptionKey)
	if err != nil {
		panic(err)
	}
	ek, err := circuits.DeserializeEncryptionKey(v)
	if err != nil {
		panic(err)
	}
	return ek
}

// AggregatorWitnessHash uses the following values for each vote
//
//	process.ID
//	process.CensusRoot
//	process.BallotMode
//	process.EncryptionKey
//	vote.Address
//	vote.Commitment
//	vote.Nullifier
//	vote.Ballot
//
// to calculate a subhash of each process+vote, then hashes all subhashes
// and returns the final hash
func (o *State) AggregatorWitnessHash() (*big.Int, error) {
	// TODO: move this func somewhere else, along with other similar funcs used by other circuits
	subhashes := []*big.Int{}
	for _, v := range o.PaddedVotes() {
		inputs := []*big.Int{}
		inputs = append(inputs, o.ProcessSerializeBigInts()...)
		inputs = append(inputs, v.SerializeBigInts()...)
		h, err := mimc7.Hash(inputs, nil)
		if err != nil {
			return nil, err
		}
		subhashes = append(subhashes, h)
	}

	hash, err := mimc7.Hash(subhashes, nil)
	if err != nil {
		return nil, err
	}
	return hash, nil
}
