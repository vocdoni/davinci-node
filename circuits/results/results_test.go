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
	StateRoot                  frontend.Variable `gnark:",public"`
	AddAccumulatorsEncrypted   circuits.Ballot
	SubAccumulatorsEncrypted   circuits.Ballot
	AddAccumulatorsMerkleProof merkleproof.MerkleProof
	SubAccumulatorsMerkleProof merkleproof.MerkleProof
	EncryptionKeyMerkleProof   merkleproof.MerkleProof
	EncryptionPublicKey        circuits.EncryptionKey[frontend.Variable]
}

func (c *encryptionKeyBindingCircuit) Define(api frontend.API) error {
	rc := ResultsVerifierCircuit{
		StateRoot:                  c.StateRoot,
		AddAccumulatorsEncrypted:   c.AddAccumulatorsEncrypted,
		SubAccumulatorsEncrypted:   c.SubAccumulatorsEncrypted,
		AddAccumulatorsMerkleProof: c.AddAccumulatorsMerkleProof,
		SubAccumulatorsMerkleProof: c.SubAccumulatorsMerkleProof,
		EncryptionKeyMerkleProof:   c.EncryptionKeyMerkleProof,
		EncryptionPublicKey:        c.EncryptionPublicKey,
	}
	rc.VerifyMerkleProofs(api)
	rc.VerifyMerkleProofLeaves(api)
	return nil
}

type resultsSubtractionCircuit struct {
	Results         [params.FieldsPerBallot]frontend.Variable `gnark:",public"`
	AddAccumulators [params.FieldsPerBallot]frontend.Variable
	SubAccumulators [params.FieldsPerBallot]frontend.Variable
}

func (c *resultsSubtractionCircuit) Define(api frontend.API) error {
	rc := ResultsVerifierCircuit{
		Results:         c.Results,
		AddAccumulators: c.AddAccumulators,
		SubAccumulators: c.SubAccumulators,
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
	encryptedAddAccumulator, addOk := st.ResultsAdd()
	c.Assert(addOk, qt.IsTrue, qt.Commentf("Add accumulator should be available"))
	encryptedSubAccumulator, subOk := st.ResultsSub()
	c.Assert(subOk, qt.IsTrue, qt.Commentf("Sub accumulator should be available"))

	// Decrypt the votes and generate the decryption proofs
	maxValue := ballotMode.MaxValue * 1000
	addAccumulator := [params.FieldsPerBallot]*big.Int{}
	addCiphertexts := [params.FieldsPerBallot]elgamal.Ciphertext{}
	addDecryptionProofs := [params.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedAddAccumulator.Ciphertexts {
		c.Assert(ct.C1 != nil && ct.C2 != nil, qt.IsTrue)
		addCiphertexts[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		addAccumulator[i] = result
		addDecryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}
	resultsAccumulator := [params.FieldsPerBallot]*big.Int{}
	subAccumulator := [params.FieldsPerBallot]*big.Int{}
	subCiphertexts := [params.FieldsPerBallot]elgamal.Ciphertext{}
	subDecryptionProofs := [params.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedSubAccumulator.Ciphertexts {
		c.Assert(ct.C1 != nil && ct.C2 != nil, qt.IsTrue)
		subCiphertexts[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		subAccumulator[i] = result
		resultsAccumulator[i] = new(big.Int).Sub(addAccumulator[i], subAccumulator[i])
		subDecryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}

	// Generate the assignment for the circuit
	assignment, err := GenerateAssignment(
		st,
		resultsAccumulator,
		addAccumulator,
		subAccumulator,
		addCiphertexts,
		subCiphertexts,
		addDecryptionProofs,
		subDecryptionProofs,
	)
	c.Assert(err, qt.IsNil)

	log.DebugTime("results inputs generation", startTime)

	assert := test.NewAssert(t)
	invalid := *assignment
	invalid.DecryptionAddProofs[0].A1.Y = big.NewInt(0)

	// Start the proving process
	startTime = time.Now()
	assert.CheckCircuit(
		&ResultsVerifierCircuit{},
		test.WithValidAssignment(assignment),
		test.WithInvalidAssignment(&invalid),
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

	encryptedAddAccumulator, addOk := st.ResultsAdd()
	c.Assert(addOk, qt.IsTrue)
	encryptedSubAccumulator, subOk := st.ResultsSub()
	c.Assert(subOk, qt.IsTrue)

	maxValue := ballotMode.MaxValue * 1000
	addAccumulator := [params.FieldsPerBallot]*big.Int{}
	addCiphertexts := [params.FieldsPerBallot]elgamal.Ciphertext{}
	addDecryptionProofs := [params.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedAddAccumulator.Ciphertexts {
		addCiphertexts[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		addAccumulator[i] = result
		addDecryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}

	resultsAccumulator := [params.FieldsPerBallot]*big.Int{}
	subAccumulator := [params.FieldsPerBallot]*big.Int{}
	subCiphertexts := [params.FieldsPerBallot]elgamal.Ciphertext{}
	subDecryptionProofs := [params.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedSubAccumulator.Ciphertexts {
		subCiphertexts[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		subAccumulator[i] = result
		resultsAccumulator[i] = new(big.Int).Sub(addAccumulator[i], subAccumulator[i])
		subDecryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}

	assignment, err := GenerateAssignment(
		st,
		resultsAccumulator,
		addAccumulator,
		subAccumulator,
		addCiphertexts,
		subCiphertexts,
		addDecryptionProofs,
		subDecryptionProofs,
	)
	c.Assert(err, qt.IsNil)

	valid := &encryptionKeyBindingCircuit{
		StateRoot:                  assignment.StateRoot,
		AddAccumulatorsEncrypted:   assignment.AddAccumulatorsEncrypted,
		SubAccumulatorsEncrypted:   assignment.SubAccumulatorsEncrypted,
		AddAccumulatorsMerkleProof: assignment.AddAccumulatorsMerkleProof,
		SubAccumulatorsMerkleProof: assignment.SubAccumulatorsMerkleProof,
		EncryptionKeyMerkleProof:   assignment.EncryptionKeyMerkleProof,
		EncryptionPublicKey:        assignment.EncryptionPublicKey,
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

func TestResultsVerifierCircuitRejectsWrappedSubtraction(t *testing.T) {
	assert := test.NewAssert(t)

	valid := &resultsSubtractionCircuit{}
	validWitness := &resultsSubtractionCircuit{}
	for i := range params.FieldsPerBallot {
		validWitness.AddAccumulators[i] = big.NewInt(0)
		validWitness.SubAccumulators[i] = big.NewInt(0)
		validWitness.Results[i] = big.NewInt(0)
	}
	validWitness.AddAccumulators[0] = big.NewInt(2)
	validWitness.SubAccumulators[0] = big.NewInt(1)
	validWitness.Results[0] = big.NewInt(1)

	invalidWitness := &resultsSubtractionCircuit{}
	for i := range params.FieldsPerBallot {
		invalidWitness.AddAccumulators[i] = big.NewInt(0)
		invalidWitness.SubAccumulators[i] = big.NewInt(0)
		invalidWitness.Results[i] = big.NewInt(0)
	}
	invalidWitness.AddAccumulators[0] = big.NewInt(0)
	invalidWitness.SubAccumulators[0] = big.NewInt(1)
	invalidWitness.Results[0] = new(big.Int).Sub(params.ResultsVerifierCurve.ScalarField(), big.NewInt(1))

	assert.CheckCircuit(valid,
		test.WithValidAssignment(validWitness),
		test.WithInvalidAssignment(invalidWitness),
		test.WithCurves(params.ResultsVerifierCurve),
		test.WithBackends(backend.GROTH16),
	)
}
