package results

import (
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
)

const nVotes = 10

type encryptionKeyBindingCircuit struct {
	StateRoot                frontend.Variable `gnark:",public"`
	AccumulatorsEncrypted    circuits.Ballot
	AccumulatorsMerkleProof  merkleproof.MerkleProof
	EncryptionKeyMerkleProof merkleproof.MerkleProof
	EncryptionPublicKey      circuits.EncryptionKey[frontend.Variable]
}

func (c *encryptionKeyBindingCircuit) Define(api frontend.API) error {
	rc := ResultsVerifierCircuit{
		StateRoot:                c.StateRoot,
		AccumulatorsEncrypted:    c.AccumulatorsEncrypted,
		AccumulatorsMerkleProof:  c.AccumulatorsMerkleProof,
		EncryptionKeyMerkleProof: c.EncryptionKeyMerkleProof,
		EncryptionPublicKey:      c.EncryptionPublicKey,
	}
	rc.VerifyMerkleProofs(api)
	rc.VerifyMerkleProofLeaves(api)
	return nil
}

type resultsComparisonCircuit struct {
	Results      [params.FieldsPerBallot]frontend.Variable `gnark:",public"`
	Accumulators [params.FieldsPerBallot]frontend.Variable
}

func (c *resultsComparisonCircuit) Define(api frontend.API) error {
	rc := ResultsVerifierCircuit{
		Results:      c.Results,
		Accumulators: c.Accumulators,
	}
	rc.VerifyResults(api)
	return nil
}

func TestResultsVerifierCircuit(t *testing.T) {
	c := qt.New(t)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())

	// Start the test
	startTime := time.Now()

	// Random inputs for the state (processID and censusRoot)
	processID := testutil.RandomProcessID()
	censusOrigin := types.CensusOriginMerkleTreeOffchainStaticV1
	ballotMode := testutil.BallotMode()
	packedBallotMode, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)

	// Generate a random ElGamal key pair
	pubKey, privKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)

	// Initialize the state
	st, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	err = st.Initialize(censusOrigin.BigInt().MathBigInt(), packedBallotMode, types.EncryptionKeyFromPoint(pubKey))
	c.Assert(err, qt.IsNil)

	// Generate a batch of random votes
	err = st.AddVotesBatch(statetest.NewVotesForTest(pubKey, nVotes, 100))
	c.Assert(err, qt.IsNil)

	// Get encrypted votes
	encryptedAccumulator, ok := st.Results()
	c.Assert(ok, qt.IsTrue, qt.Commentf("Results accumulator should be available"))

	// Decrypt the votes and generate the decryption proofs
	maxValue := ballotMode.MaxValue * 1000
	accumulator := [params.FieldsPerBallot]*big.Int{}
	accumulatorsEncrypted := [params.FieldsPerBallot]elgamal.Ciphertext{}
	decryptionProofs := [params.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedAccumulator.Ciphertexts {
		c.Assert(ct.C1 != nil && ct.C2 != nil, qt.IsTrue)
		accumulatorsEncrypted[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		accumulator[i] = result
		decryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}

	// Generate the assignment for the circuit
	assignment, err := GenerateAssignment(
		st,
		accumulator,
		accumulator,
		accumulatorsEncrypted,
		decryptionProofs,
	)
	c.Assert(err, qt.IsNil)

	log.DebugTime("results inputs generation", startTime)

	assert := test.NewAssert(t)
	invalid := *assignment
	invalid.DecryptionProofs[0].A1.Y = big.NewInt(0)

	// subgroup order used by the ElGamal scalar arithmetic
	q := new(big.Int).Set(curves.New(bjj.CurveType).Order())

	shiftedAssignment := func(slot int) *ResultsVerifierCircuit {
		honestAccumulator := new(big.Int).Set(accumulator[slot])

		// Malicious witness: same ciphertext and same proof, but plaintext shifted by q.
		shifted := *assignment
		shifted.Accumulators[slot] = new(big.Int).Add(honestAccumulator, q)
		shifted.Results[slot] = new(big.Int).Add(honestAccumulator, q)

		c.Assert(shifted.Results[slot].(*big.Int).Cmp(assignment.Results[slot].(*big.Int)), qt.Not(qt.Equals), 0)

		// Human-readable explanation of the bug
		c.Logf("[BUG DEMO] slot=%d\n  honest result:  %s\n  shifted result: %s (= honest + subgroup order)",
			slot, honestAccumulator.String(), shifted.Results[slot].(*big.Int).String())
		return &shifted
	}
	startTime = time.Now()
	assert.CheckCircuit(
		&ResultsVerifierCircuit{},
		test.WithValidAssignment(assignment),
		test.WithInvalidAssignment(&invalid),
		test.WithInvalidAssignment(shiftedAssignment(0)),
		test.WithInvalidAssignment(shiftedAssignment(1)),
		test.WithCurves(params.ResultsVerifierCurve),
		test.WithBackends(backend.GROTH16),
	)
	log.DebugTime("results proving", startTime)
}

func TestResultsVerifierCircuitBindsEncryptionKeyToMerkleLeaf(t *testing.T) {
	c := qt.New(t)

	processID := testutil.RandomProcessID()
	censusOrigin := types.CensusOriginMerkleTreeOffchainStaticV1
	ballotMode := testutil.BallotMode()
	packedBallotMode, err := ballotMode.Pack()
	c.Assert(err, qt.IsNil)

	pubKey, privKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)

	st, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	err = st.Initialize(censusOrigin.BigInt().MathBigInt(), packedBallotMode, types.EncryptionKeyFromPoint(pubKey))
	c.Assert(err, qt.IsNil)

	err = st.AddVotesBatch(statetest.NewVotesForTest(pubKey, nVotes, 100))
	c.Assert(err, qt.IsNil)

	encryptedAccumulator, ok := st.Results()
	c.Assert(ok, qt.IsTrue)

	maxValue := ballotMode.MaxValue * 1000
	accumulator := [params.FieldsPerBallot]*big.Int{}
	accumulatorsEncrypted := [params.FieldsPerBallot]elgamal.Ciphertext{}
	decryptionProofs := [params.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedAccumulator.Ciphertexts {
		accumulatorsEncrypted[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		accumulator[i] = result
		decryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}

	assignment, err := GenerateAssignment(
		st,
		accumulator,
		accumulator,
		accumulatorsEncrypted,
		decryptionProofs,
	)
	c.Assert(err, qt.IsNil)

	valid := &encryptionKeyBindingCircuit{
		StateRoot:                assignment.StateRoot,
		AccumulatorsEncrypted:    assignment.AccumulatorsEncrypted,
		AccumulatorsMerkleProof:  assignment.AccumulatorsMerkleProof,
		EncryptionKeyMerkleProof: assignment.EncryptionKeyMerkleProof,
		EncryptionPublicKey:      assignment.EncryptionPublicKey,
	}

	otherPubKey, _, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)
	otherX, otherY := otherPubKey.Point()
	invalid := *valid
	invalid.EncryptionPublicKey.PubKey = [2]frontend.Variable{
		otherX,
		otherY,
	}

	assert := test.NewAssert(t)
	assert.CheckCircuit(
		&encryptionKeyBindingCircuit{},
		test.WithValidAssignment(valid),
		test.WithInvalidAssignment(&invalid),
		test.WithCurves(params.ResultsVerifierCurve),
		test.WithBackends(backend.GROTH16),
	)
}

func TestResultsVerifierCircuitRejectsWrappedAccumulator(t *testing.T) {
	assert := test.NewAssert(t)

	valid := &resultsComparisonCircuit{}
	validWitness := &resultsComparisonCircuit{}
	for i := range params.FieldsPerBallot {
		validWitness.Accumulators[i] = big.NewInt(0)
		validWitness.Results[i] = big.NewInt(0)
	}
	validWitness.Accumulators[0] = big.NewInt(1)
	validWitness.Results[0] = big.NewInt(1)

	invalidWitness := &resultsComparisonCircuit{}
	for i := range params.FieldsPerBallot {
		invalidWitness.Accumulators[i] = big.NewInt(0)
		invalidWitness.Results[i] = big.NewInt(0)
	}
	invalidWitness.Accumulators[0] = big.NewInt(0)
	invalidWitness.Results[0] = new(big.Int).Sub(params.ResultsVerifierCurve.ScalarField(), big.NewInt(1))

	assert.CheckCircuit(valid,
		test.WithValidAssignment(validWitness),
		test.WithInvalidAssignment(invalidWitness),
		test.WithCurves(params.ResultsVerifierCurve),
		test.WithBackends(backend.GROTH16),
	)
}
