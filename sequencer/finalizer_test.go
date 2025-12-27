package sequencer

import (
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	qt "github.com/frankban/quicktest"
	"github.com/vocdoni/davinci-node/circuits/results"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/db"
	"github.com/vocdoni/davinci-node/db/metadb"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/storage"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func loadResultsVerifierArtifactsForTest(t *testing.T) *internalCircuits {
	t.Helper()
	ca := new(internalCircuits)
	err := results.Artifacts.DownloadAll(t.Context())
	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to download results verifier artifacts: %v", err))
	ca.rvCcs, ca.rvPk, err = loadCircuitArtifacts(results.Artifacts)
	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to load results verifier artifacts: %v", err))
	return ca
}

// TestFinalize tests the finalize method of the Finalizer struct
func TestFinalize(t *testing.T) {
	c := qt.New(t)

	// Setup test environment
	stg, stateDB, pid, _, _, _, cleanup := setupTestEnvironment(t, 10000, 5000)
	defer cleanup()

	// Create a finalizer
	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil, nil)
	f.Start(t.Context(), 0)

	// Test finalize
	f.OndemandCh <- pid.Marshal()
	_, err := f.WaitUntilResults(t.Context(), pid)
	c.Assert(err, qt.IsNil, qt.Commentf("finalize failed: %v", err))

	// Check that the process has been updated with the result
	process, err := stg.Process(pid)
	c.Assert(err, qt.IsNil)
	c.Assert(process.Result, qt.Not(qt.IsNil))

	// Verify the results are as expected
	c.Assert(len(process.Result), qt.Equals, types.FieldsPerBallot)
	expected := big.NewInt(5000)
	c.Assert(process.Result[0].MathBigInt().Cmp(expected), qt.Equals, 0,
		qt.Commentf("Expected first result to be 500, got %s", process.Result[0].String()))
}

// setupTestEnvironment creates a test environment with necessary objects
func setupTestEnvironment(t *testing.T, addValue, subValue int64) (
	*storage.Storage,
	db.Database,
	*types.ProcessID,
	ecc.Point,
	ecc.Point,
	*big.Int,
	func(),
) {
	// Create temporary directory
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "db")

	// Create database
	mainDB, err := metadb.New(db.TypePebble, dbPath)
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}

	// Create storage
	stg := storage.New(mainDB)

	// Get state database
	stateDB := stg.StateDB()

	// Create a process ID
	pid := &types.ProcessID{
		Address: common.Address{},
		Nonce:   42,
		Version: []byte{0x00, 0x00, 0x00, 0x01},
	}

	// Create encryption keys
	curve := curves.New(bjj.CurveType)
	pubKey, privKey, err := elgamal.GenerateKey(curve)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Store the keys in storage
	err = stg.SetEncryptionKeys(pid, pubKey, privKey)
	if err != nil {
		t.Fatalf("failed to store encryption keys: %v", err)
	}
	// Create a process
	process := &types.Process{
		ID:          pid.Marshal(),
		Status:      0,
		StartTime:   time.Now(),
		Duration:    time.Hour,
		MetadataURI: "http://example.com/metadata",
		StateRoot:   new(types.BigInt).SetUint64(100),
		BallotMode: &types.BallotMode{
			NumFields:      8,
			MaxValue:       new(types.BigInt).SetUint64(100),
			MinValue:       new(types.BigInt).SetUint64(0),
			MaxValueSum:    new(types.BigInt).SetUint64(0),
			MinValueSum:    new(types.BigInt).SetUint64(0),
			UniqueValues:   false,
			CostFromWeight: false,
		},
		Census: &types.Census{
			CensusRoot:   make([]byte, 32),
			CensusURI:    "http://example.com/census",
			CensusOrigin: types.CensusOriginMerkleTreeOffchainStaticV1,
		},
	}

	// Store the process
	err = stg.NewProcess(process)
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

	process, err = stg.Process(pid)
	if err != nil {
		t.Fatalf("failed to get process: %v", err)
	}

	// Setup state with test data
	process.StateRoot = setupTestState(t, stateDB, pid, pubKey, process.StateRoot.MathBigInt(), addValue, subValue)
	err = stg.UpdateProcess(pid, func(p *types.Process) error {
		p.StateRoot = process.StateRoot
		return nil
	})
	if err != nil {
		t.Fatalf("failed to store process: %v", err)
	}

	// Return cleanup function
	cleanup := func() {
		stg.Close()
	}

	return stg, stateDB, pid, curve, pubKey, privKey, cleanup
}

// setupTestState initializes the state with encrypted test data
func setupTestState(
	t *testing.T,
	stateDB db.Database,
	pid *types.ProcessID,
	pubKey ecc.Point,
	stateRoot *big.Int,
	addValue, subValue int64,
) *types.BigInt {
	// Load the initial state
	st, err := state.LoadOnRoot(stateDB, pid.BigInt(), stateRoot)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	// Create encrypted accumulators with known values
	curve := pubKey.New()

	// Add accumulator with a known value
	addAccumulator := elgamal.NewBallot(curve)
	addValues := [types.FieldsPerBallot]*big.Int{}
	// Set values for testing
	for i := range types.FieldsPerBallot {
		addValues[i] = big.NewInt(addValue)
	}
	// Encrypt the values
	k1 := new(big.Int).SetBytes(util.RandomBytes(16))
	encryptedAdd, err := addAccumulator.Encrypt(addValues, pubKey, k1)
	if err != nil {
		t.Fatalf("failed to encrypt add accumulator: %v", err)
	}

	// Sub accumulator with a known value
	subAccumulator := elgamal.NewBallot(curve)
	subValues := [types.FieldsPerBallot]*big.Int{}
	// Set values for testing
	for i := range types.FieldsPerBallot {
		subValues[i] = big.NewInt(subValue)
	}
	// Encrypt the values
	k2 := new(big.Int).SetBytes(util.RandomBytes(16))
	encryptedSub, err := subAccumulator.Encrypt(subValues, pubKey, k2)
	if err != nil {
		t.Fatalf("failed to encrypt sub accumulator: %v", err)
	}

	// Store the encrypted accumulators in the state
	st.SetResultsAdd(encryptedAdd)
	st.SetResultsSub(encryptedSub)

	stateRoot, err = st.RootAsBigInt()
	if err != nil {
		t.Fatalf("failed to get state root: %v", err)
	}
	return (*types.BigInt)(stateRoot)
}
