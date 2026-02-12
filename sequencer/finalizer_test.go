package sequencer

// import (
// 	"math/big"
// 	"path/filepath"
// 	"testing"
// 	"time"

// 	qt "github.com/frankban/quicktest"
// 	"github.com/vocdoni/davinci-node/circuits/results"
// 	"github.com/vocdoni/davinci-node/crypto/ecc"
// 	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
// 	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
// 	"github.com/vocdoni/davinci-node/crypto/elgamal"
// 	"github.com/vocdoni/davinci-node/db"
// 	"github.com/vocdoni/davinci-node/db/metadb"
// 	"github.com/vocdoni/davinci-node/internal/testutil"
// 	"github.com/vocdoni/davinci-node/state"
// 	"github.com/vocdoni/davinci-node/storage"
// 	"github.com/vocdoni/davinci-node/types"
// 	"github.com/vocdoni/davinci-node/types/params"
// )

// func loadResultsVerifierArtifactsForTest(t *testing.T) *internalCircuits {
// 	t.Helper()
// 	ca := new(internalCircuits)
// 	err := results.Artifacts.DownloadAll(t.Context())
// 	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to download results verifier artifacts: %v", err))
// 	ca.rvCcs, ca.rvPk, err = loadCircuitArtifacts(results.Artifacts)
// 	qt.Assert(t, err, qt.IsNil, qt.Commentf("failed to load results verifier artifacts: %v", err))
// 	return ca
// }

// // TestFinalize tests the finalize method of the Finalizer struct
// func TestFinalize(t *testing.T) {
// 	c := qt.New(t)

// 	// Setup test environment
// 	stg, stateDB, procesSID, _, _, _, cleanup := setupTestEnvironment(t, 10000, 5000)
// 	defer cleanup()

// 	// Create a finalizer
// 	f := newFinalizer(stg, stateDB, loadResultsVerifierArtifactsForTest(t), nil, nil)
// 	f.Start(t.Context(), 0)

// 	// Test finalize
// 	f.OndemandCh <- procesSID
// 	_, err := f.WaitUntilResults(t.Context(), procesSID)
// 	c.Assert(err, qt.IsNil, qt.Commentf("finalize failed: %v", err))

// 	// Check that the process has been updated with the result
// 	process, err := stg.Process(procesSID)
// 	c.Assert(err, qt.IsNil)
// 	c.Assert(process.Result, qt.Not(qt.IsNil))

// 	// Verify the results are as expected
// 	c.Assert(len(process.Result), qt.Equals, params.FieldsPerBallot)
// 	expected := big.NewInt(5000)
// 	c.Assert(process.Result[0].MathBigInt().Cmp(expected), qt.Equals, 0,
// 		qt.Commentf("Expected first result to be 500, got %s", process.Result[0].String()))
// }

// // setupTestEnvironment creates a test environment with necessary objects
// func setupTestEnvironment(t *testing.T, addValue, subValue int64) (
// 	*storage.Storage,
// 	db.Database,
// 	types.ProcessID,
// 	ecc.Point,
// 	ecc.Point,
// 	*big.Int,
// 	func(),
// ) {
// 	// Create temporary directory
// 	tempDir := t.TempDir()
// 	dbPath := filepath.Join(tempDir, "db")

// 	// Create database
// 	mainDB, err := metadb.New(db.TypePebble, dbPath)
// 	if err != nil {
// 		t.Fatalf("failed to create database: %v", err)
// 	}

// 	// Create storage
// 	stg := storage.New(mainDB)

// 	// Get state database
// 	stateDB := stg.StateDB()

// 	// Create a process ID
// 	processID := testutil.DeterministicProcessID(42)

// 	// Create encryption keys
// 	curve := curves.New(bjj.CurveType)
// 	pubKey, privKey, err := elgamal.GenerateKey(curve)
// 	if err != nil {
// 		t.Fatalf("failed to generate key: %v", err)
// 	}

// 	// Store the keys in storage
// 	err = stg.SetEncryptionKeys(processID, pubKey, privKey)
// 	if err != nil {
// 		t.Fatalf("failed to store encryption keys: %v", err)
// 	}
// 	// Create a process
// 	process := &types.Process{
// 		ID:          &processID,
// 		Status:      0,
// 		StartTime:   time.Now(),
// 		Duration:    time.Hour,
// 		MetadataURI: "http://example.com/metadata",
// 		StateRoot:   testutil.StateRoot(),
// 		BallotMode:  testutil.BallotModeInternal(),
// 		Census:      testutil.RandomCensus(types.CensusOriginMerkleTreeOffchainStaticV1),
// 	}

// 	// Store the process
// 	err = stg.NewProcess(process)
// 	if err != nil {
// 		t.Fatalf("failed to store process: %v", err)
// 	}

// 	process, err = stg.Process(processID)
// 	if err != nil {
// 		t.Fatalf("failed to get process: %v", err)
// 	}

// 	// Setup state with test data
// 	process.StateRoot = setupTestState(t, stateDB, processID, pubKey, process.StateRoot.MathBigInt(), addValue, subValue)
// 	err = stg.UpdateProcess(processID, func(p *types.Process) error {
// 		p.StateRoot = process.StateRoot
// 		return nil
// 	})
// 	if err != nil {
// 		t.Fatalf("failed to store process: %v", err)
// 	}

// 	// Return cleanup function
// 	cleanup := func() {
// 		stg.Close()
// 	}

// 	return stg, stateDB, processID, curve, pubKey, privKey, cleanup
// }

// // setupTestState initializes the state with encrypted test data
// func setupTestState(
// 	t *testing.T,
// 	stateDB db.Database,
// 	processID types.ProcessID,
// 	pubKey ecc.Point,
// 	stateRoot *big.Int,
// 	addValue, subValue int64,
// ) *types.BigInt {
// 	// Load the initial state
// 	st, err := state.LoadOnRoot(stateDB, processID, stateRoot)
// 	if err != nil {
// 		t.Fatalf("failed to load state: %v", err)
// 	}

// 	// Create encrypted accumulators with known values
// 	curve := pubKey.New()

// 	// Add accumulator with a known value
// 	addAccumulator := elgamal.NewBallot(curve)
// 	addValues := [params.FieldsPerBallot]*big.Int{}
// 	// Set values for testing
// 	for i := range params.FieldsPerBallot {
// 		addValues[i] = big.NewInt(addValue)
// 	}
// 	// Encrypt the values
// 	k1, err := elgamal.RandK()
// 	if err != nil {
// 		t.Fatalf("failed to generate k1: %v", err)
// 	}
// 	encryptedAdd, err := addAccumulator.Encrypt(addValues, pubKey, k1)
// 	if err != nil {
// 		t.Fatalf("failed to encrypt add accumulator: %v", err)
// 	}

// 	// Sub accumulator with a known value
// 	subAccumulator := elgamal.NewBallot(curve)
// 	subValues := [params.FieldsPerBallot]*big.Int{}
// 	// Set values for testing
// 	for i := range params.FieldsPerBallot {
// 		subValues[i] = big.NewInt(subValue)
// 	}
// 	// Encrypt the values
// 	k2, err := elgamal.RandK()
// 	if err != nil {
// 		t.Fatalf("failed to generate k2: %v", err)
// 	}
// 	encryptedSub, err := subAccumulator.Encrypt(subValues, pubKey, k2)
// 	if err != nil {
// 		t.Fatalf("failed to encrypt sub accumulator: %v", err)
// 	}

// 	// Store the encrypted accumulators in the state
// 	st.SetResultsAdd(encryptedAdd)
// 	st.SetResultsSub(encryptedSub)

// 	stateRoot, err = st.RootAsBigInt()
// 	if err != nil {
// 		t.Fatalf("failed to get state root: %v", err)
// 	}
// 	return (*types.BigInt)(stateRoot)
// }
