package state

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/vocdoni/arbo"
	"github.com/vocdoni/davinci-node/circuits"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/prefixeddb"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
)

var (
	// HashFn is the hash function used in the state tree.
	HashFn = arbo.HashFunctionMultiPoseidon
	// Curve is the curve used for the encryption
	Curve = curves.New(bjj.CurveType)
)

var (
	KeyProcessID     = types.StateKey(params.StateKeyProcessID)
	KeyBallotMode    = types.StateKey(params.StateKeyBallotMode)
	KeyEncryptionKey = types.StateKey(params.StateKeyEncryptionKey)
	KeyResults       = types.StateKey(params.StateKeyResults)
	KeyCensusOrigin  = types.StateKey(params.StateKeyCensusOrigin)

	ErrStateAlreadyInitialized = fmt.Errorf("state already initialized")
)

// State represents a state tree
type State struct {
	tree      *arbo.Tree
	processID types.ProcessID
	db        db.Database
}

// ProcessProofs stores the Merkle proofs for the process, including the ID
// census root, ballot mode, and encryption key proofs.
type ProcessProofs struct {
	ID            *ArboProof
	CensusOrigin  *ArboProof
	BallotMode    *ArboProof
	EncryptionKey *ArboProof
}

// VotesProofs stores the Merkle transitions for the votes:
// the results transition, as well as the ballot and vote ID transitions.
type VotesProofs struct {
	Results *ArboTransition
	Ballot  [params.VotesPerBatch]*ArboTransition
	VoteID  [params.VotesPerBatch]*ArboTransition
}

// New creates or opens a State stored in the passed database.
// The processID is used as a prefix for the keys in the database.
func New(db db.Database, processID types.ProcessID) (*State, error) {
	if !processID.BigInt().IsInField(params.StateTransitionCurve.ScalarField()) {
		return nil, fmt.Errorf("processID is not in field")
	}
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

// LoadSnapshotOnRoot loads a read-only State view at the provided root.
func LoadSnapshotOnRoot(db db.Database, processID types.ProcessID, root *big.Int) (*State, error) {
	pdb, tree, rootBytes, err := openTreeForRootCheck(db, processID, root)
	if err != nil {
		return nil, err
	}
	if err := tree.RootExists(rootBytes); err != nil {
		return nil, fmt.Errorf("could not find root in state: %w", err)
	}
	snapshot, err := tree.Snapshot(rootBytes)
	if err != nil {
		return nil, fmt.Errorf("could not snapshot state root: %w", err)
	}
	return &State{
		db:        pdb,
		tree:      snapshot,
		processID: processID,
	}, nil
}

// RootExists checks if the provided root exists in the tree for the given
// processId. Returns nil if the root exists, or an error if it does not.
func RootExists(db db.Database, processID types.ProcessID, root *big.Int) error {
	_, tree, rootBytes, err := openTreeForRootCheck(db, processID, root)
	if err != nil {
		return err
	}
	if err := tree.RootExists(rootBytes); err != nil {
		return fmt.Errorf("could not find root in state: %w", err)
	}
	return nil
}

func openTreeForRootCheck(database db.Database, processID types.ProcessID, root *big.Int) (db.Database, *arbo.Tree, []byte, error) {
	if root == nil {
		return nil, nil, nil, fmt.Errorf("nil state root")
	}

	if !processID.IsValid() {
		return nil, nil, nil, fmt.Errorf("processID is not valid")
	}

	pdb := prefixeddb.NewPrefixedDatabase(database, processID.Bytes())
	tx := arbo.NewTreeWriteTx(pdb)
	defer tx.Discard()
	tree, err := arbo.NewTreeWithTx(tx, arbo.Config{
		Database:     pdb,
		MaxLevels:    params.StateTreeMaxLevels,
		HashFunction: HashFn,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("could not open state: %w", err)
	}
	return pdb, tree, BigIntToBytes(root), nil
}

type stateTreeTx struct {
	state *State
	tx    db.WriteTx
}

func (s *State) newTreeTx() *stateTreeTx {
	return &stateTreeTx{
		state: s,
		tx:    s.tree.WriteTx(),
	}
}

func (tx *stateTreeTx) commit(op string) error {
	if tx.tx == nil {
		return fmt.Errorf("%s: no active state transaction", op)
	}
	activeTx := tx.tx
	if err := activeTx.Commit(); err != nil {
		activeTx.Discard()
		tx.tx = nil
		return fmt.Errorf("%s: %w", op, err)
	}
	activeTx.Discard()
	tx.tx = nil
	return nil
}

func (tx *stateTreeTx) discard() {
	if tx.tx == nil {
		return
	}
	tx.tx.Discard()
	tx.tx = nil
}

func (s *State) getBigInt(key *big.Int) (*big.Int, []*big.Int, error) {
	return s.tree.GetBigInt(key)
}

func (s *State) addBigInt(key *big.Int, values ...*big.Int) error {
	return s.tree.AddBigInt(key, values...)
}

func (s *State) updateBigInt(key *big.Int, values ...*big.Int) error {
	return s.tree.UpdateBigInt(key, values...)
}

func (s *State) generateGnarkVerifierProofBigInt(key *big.Int) (*arbo.GnarkVerifierProof, error) {
	return s.tree.GenerateGnarkVerifierProofBigInt(key)
}

func (tx *stateTreeTx) getBigInt(key *big.Int) (*big.Int, []*big.Int, error) {
	if tx.tx == nil {
		return nil, nil, fmt.Errorf("state transaction is not active")
	}
	return tx.state.tree.GetBigIntWithTx(tx.tx, key)
}

func (tx *stateTreeTx) addBigInt(key *big.Int, values ...*big.Int) error {
	if tx.tx == nil {
		return fmt.Errorf("state transaction is not active")
	}
	return tx.state.tree.AddBigIntWithTx(tx.tx, key, values...)
}

func (tx *stateTreeTx) updateBigInt(key *big.Int, values ...*big.Int) error {
	if tx.tx == nil {
		return fmt.Errorf("state transaction is not active")
	}
	return tx.state.tree.UpdateBigIntWithTx(tx.tx, key, values...)
}

func (tx *stateTreeTx) RootAsBigInt() (*big.Int, error) {
	if tx.tx == nil {
		return nil, fmt.Errorf("state transaction is not active")
	}
	root, err := tx.state.tree.RootWithTx(tx.tx)
	if err != nil {
		return nil, fmt.Errorf("get state root: %w", err)
	}
	return BytesToBigInt(root), nil
}

func (tx *stateTreeTx) SetRootAsBigInt(newRoot *big.Int) error {
	if tx.tx == nil {
		return fmt.Errorf("state transaction is not active")
	}
	if newRoot == nil {
		return fmt.Errorf("nil state root")
	}
	if err := tx.state.tree.SetRootWithTx(tx.tx, BigIntToBytes(newRoot)); err != nil {
		return fmt.Errorf("set state root: %w", err)
	}
	return nil
}

func (tx *stateTreeTx) setResults(results *elgamal.Ballot) error {
	if results == nil {
		return fmt.Errorf("nil results")
	}
	return tx.updateBigInt(KeyResults.BigInt(), results.BigInts()...)
}

// Initialize creates a new State, initialized with the passed parameters.
func (s *State) Initialize(
	censusOrigin *big.Int,
	ballotMode *big.Int,
	encryptionKey types.EncryptionKey,
) (err error) {
	treeTx := s.newTreeTx()
	defer func() {
		if err != nil {
			treeTx.discard()
		}
	}()

	// Check if the state is already initialized
	// TODO: refactor arbo to use uint64 instead
	if _, _, err := treeTx.getBigInt(KeyProcessID.BigInt()); err == nil {
		return ErrStateAlreadyInitialized
	} else if !errors.Is(err, arbo.ErrKeyNotFound) {
		return fmt.Errorf("check state initialization: %w", err)
	}
	if err := treeTx.addBigInt(KeyProcessID.BigInt(), s.processID.MathBigInt()); err != nil {
		return fmt.Errorf("could not set process ID: %w", err)
	}
	if err := treeTx.addBigInt(KeyBallotMode.BigInt(), ballotMode); err != nil {
		return fmt.Errorf("could not set ballot mode: %w", err)
	}
	if err := treeTx.addBigInt(KeyEncryptionKey.BigInt(), encryptionKey.BigInts()...); err != nil {
		return fmt.Errorf("could not set encryption key: %w", err)
	}
	if err := treeTx.addBigInt(KeyResults.BigInt(), elgamal.NewBallot(Curve).BigInts()...); err != nil {
		return fmt.Errorf("could not set results: %w", err)
	}
	if err := treeTx.addBigInt(KeyCensusOrigin.BigInt(), censusOrigin); err != nil {
		return fmt.Errorf("could not set census origin: %w", err)
	}
	return treeTx.commit("initialize state")
}

func (s *State) AddVotesBatch(votes []*Vote) error {
	batch, err := s.PrepareVotesBatch(votes)
	if err != nil {
		return err
	}
	return batch.Commit()
}

// RootAsBigInt method returns the root of the tree as a big.Int.
func (s *State) RootAsBigInt() (*big.Int, error) {
	root, err := s.tree.Root()
	if err != nil {
		return nil, fmt.Errorf("get state root: %w", err)
	}
	return BytesToBigInt(root), nil
}

// SetRootAsBigInt method sets the root of the tree to the provided one as a
// big.Int.
func (s *State) SetRootAsBigInt(newRoot *big.Int) error {
	if newRoot == nil {
		return fmt.Errorf("nil state root")
	}
	root := BigIntToBytes(newRoot)
	if err := s.tree.SetRoot(root); err != nil {
		return fmt.Errorf("set state root: %w", err)
	}
	return nil
}

// RootExists checks if the provided root exists in the tree.
// Returns nil if the root exists, or an error if it does not.
func (s *State) RootExists(root *big.Int) error {
	if root == nil {
		return fmt.Errorf("nil state root")
	}
	return s.tree.RootExists(BigIntToBytes(root))
}

// Process returns all process details from the state.
func (s *State) Process() circuits.Process[*big.Int] {
	return circuits.Process[*big.Int]{
		ID:            s.ProcessID(),
		CensusOrigin:  s.CensusOrigin(),
		BallotMode:    s.BallotMode(),
		EncryptionKey: s.EncryptionKey(),
	}
}

// ProcessID returns the process ID of the state as a big.Int.
func (s *State) ProcessID() *big.Int {
	_, v, err := s.getBigInt(KeyProcessID.BigInt())
	if err != nil {
		log.Errorw(err, "failed to get process ID from state")
	}
	if len(v) == 0 {
		return big.NewInt(0) // default value if not set
	}
	return v[0]
}

// CensusOrigin returns the census origin of the state as a *big.Int.
func (s *State) CensusOrigin() *big.Int {
	_, v, err := s.getBigInt(KeyCensusOrigin.BigInt())
	if err != nil {
		log.Errorw(err, "failed to get census origin from state")
	}
	if len(v) == 0 {
		return big.NewInt(0) // default value if not set
	}
	return v[0]
}

// BallotMode returns the packed ballot mode of the state as a *big.Int.
func (s *State) BallotMode() *big.Int {
	_, v, err := s.getBigInt(KeyBallotMode.BigInt())
	if err != nil {
		log.Errorw(err, "failed to get ballot mode from state")
	}
	if len(v) == 0 {
		return big.NewInt(0)
	}
	return v[0]
}

// EncryptionKey returns the encryption key of the state as a
// circuits.EncryptionKey[*big.Int].
func (s *State) EncryptionKey() circuits.EncryptionKey[*big.Int] {
	_, v, err := s.getBigInt(KeyEncryptionKey.BigInt())
	if err != nil {
		log.Errorw(err, "failed to get encryption key from state")
	}
	ek, err := new(circuits.EncryptionKey[*big.Int]).Deserialize(v)
	if err != nil {
		log.Errorw(err, "failed to deserialize encryption key in state")
	}
	return ek
}

// Results returns the results of the state as an elgamal.Ballot.
func (s *State) Results() (*elgamal.Ballot, error) {
	return resultsFromTree(s)
}

func resultsFromTree(reader stateValueReader) (*elgamal.Ballot, error) {
	_, v, err := reader.getBigInt(KeyResults.BigInt())
	if err != nil {
		return nil, fmt.Errorf("failed to get results from state: %w", err)
	}
	return elgamal.NewBallot(Curve).SetBigInts(v)
}

// SetResults sets the results directly in the state tree.
func (s *State) SetResults(results *elgamal.Ballot) error {
	if results == nil {
		return fmt.Errorf("nil results")
	}
	return s.updateBigInt(KeyResults.BigInt(), results.BigInts()...)
}

// EncodeKey encodes a key to a byte array using the maximum key length for the
// current number of levels in the state tree and the hash function length.
func EncodeKey(key *big.Int) []byte {
	maxKeyLen := arbo.MaxKeyLen(params.StateTreeMaxLevels, HashFn.Len())
	return arbo.BigIntToBytes(maxKeyLen, key)
}
