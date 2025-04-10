package state

import (
	"fmt"
	"log"
	"math/big"
	"slices"

	"github.com/iden3/go-iden3-crypto/mimc7"
	"github.com/vocdoni/arbo"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	bjj "github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/ecc/curves"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"go.vocdoni.io/dvote/db"
	"go.vocdoni.io/dvote/db/prefixeddb"
)

var (
	// HashFunc is the hash function used in the state tree.
	HashFunc = arbo.HashFunctionMultiPoseidon
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
)

// State represents a state tree
type State struct {
	tree      *arbo.Tree
	processID *big.Int
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
	Ballot     [circuits.VotesPerBatch]*ArboTransition
	Commitment [circuits.VotesPerBatch]*ArboTransition
}

// New creates or opens a State stored in the passed database.
// The processId is used as a prefix for the keys in the database.
func New(db db.Database, processId *big.Int) (*State, error) {
	pdb := prefixeddb.NewPrefixedDatabase(db, processId.Bytes())
	tree, err := arbo.NewTree(arbo.Config{
		Database:     pdb,
		MaxLevels:    circuits.StateTreeMaxLevels,
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
func (o *State) Initialize(
	censusRoot *big.Int,
	ballotMode circuits.BallotMode[*big.Int],
	encryptionKey circuits.EncryptionKey[*big.Int],
) error {
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

	o.Process.ID = o.processID
	o.Process.CensusRoot = censusRoot
	o.Process.BallotMode = ballotMode
	o.Process.EncryptionKey = encryptionKey
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
		_, v, err := o.tree.GetBigInt(KeyResultsAdd)
		if err != nil {
			return err
		}
		if o.OldResultsAdd, err = o.OldResultsAdd.SetBigInts(v); err != nil {
			return fmt.Errorf("OldResultsAdd: %w", err)
		}
	}
	{
		_, v, err := o.tree.GetBigInt(KeyResultsSub)
		if err != nil {
			return err
		}
		if o.OldResultsSub, err = o.OldResultsSub.SetBigInts(v); err != nil {
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
		log.Println("Error getting ID proof:", err)
		return err
	}
	if o.ProcessProofs.CensusRoot, err = o.GenArboProof(KeyCensusRoot); err != nil {
		log.Println("Error getting CensusRoot proof:", err)
		return err
	}
	if o.ProcessProofs.BallotMode, err = o.GenArboProof(KeyBallotMode); err != nil {
		log.Println("Error getting BallotMode proof:", err)
		return err
	}
	if o.ProcessProofs.EncryptionKey, err = o.GenArboProof(KeyEncryptionKey); err != nil {
		log.Println("Error getting EncryptionKey proof:", err)
		return err
	}

	// now build ordered chain of MerkleTransitions

	// add Ballots
	for i := range o.VotesProofs.Ballot {
		if i < len(o.Votes()) {
			o.VotesProofs.Ballot[i], err = ArboTransitionFromAddOrUpdate(o,
				o.Votes()[i].Nullifier, o.Votes()[i].Ballot.BigInts()...)
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
				o.Votes()[i].Address, o.Votes()[i].Commitment)
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
		KeyResultsAdd, o.NewResultsAdd.BigInts()...)
	if err != nil {
		return fmt.Errorf("ResultsAdd: %w", err)
	}

	// update ResultsSub
	o.NewResultsSub = o.NewResultsSub.Add(o.OldResultsSub, o.OverwriteSum)
	o.VotesProofs.ResultsSub, err = ArboTransitionFromAddOrUpdate(o,
		KeyResultsSub, o.NewResultsSub.BigInts()...)
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
	for len(v) < circuits.VotesPerBatch {
		v = append(v, elgamal.NewBallot(Curve))
	}
	return v
}

func (o *State) PaddedVotes() []*Vote {
	v := slices.Clone(o.votes)
	for len(v) < circuits.VotesPerBatch {
		v = append(v, &Vote{
			Address:    big.NewInt(0),
			Commitment: big.NewInt(0),
			Nullifier:  big.NewInt(0),
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
	list = append(list, o.ProcessID())
	list = append(list, o.CensusRoot())
	list = append(list, o.BallotMode().Serialize()...)
	list = append(list, o.EncryptionKey().Serialize()...)
	return list
}

func (o *State) ProcessID() *big.Int {
	_, v, err := o.tree.GetBigInt(KeyProcessID)
	if err != nil {
		panic(err)
	}
	return v[0]
}

func (o *State) CensusRoot() *big.Int {
	_, v, err := o.tree.GetBigInt(KeyCensusRoot)
	if err != nil {
		panic(err)
	}
	return v[0]
}

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

func EncodeKey(key *big.Int) []byte {
	maxKeyLen := arbo.MaxKeyLen(circuits.StateTreeMaxLevels, HashFunc.Len())
	return arbo.BigIntToBytes(maxKeyLen, key)
}
