package results

import (
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/consensys/gnark/backend"
	"github.com/consensys/gnark/logger"
	"github.com/consensys/gnark/test"
	qt "github.com/frankban/quicktest"
	"github.com/rs/zerolog"
	"github.com/vocdoni/arbo/memdb"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/crypto/ecc"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

const nVotes = 10

func TestResultsVerifierCircuit(t *testing.T) {
	c := qt.New(t)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())

	// Start the test
	now := time.Now()

	// Random inputs for the state (processID and censusRoot)
	processID, err := rand.Int(rand.Reader, circuits.ResultsVerifierCurve.ScalarField())
	c.Assert(err, qt.IsNil)
	censusRoot, err := rand.Int(rand.Reader, circuits.ResultsVerifierCurve.ScalarField())
	c.Assert(err, qt.IsNil)
	ballotMode := circuits.MockBallotMode()

	// Generate a random ElGamal key pair
	pubKey, privKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)
	encryptionKeys := circuits.EncryptionKeyFromECCPoint(pubKey)

	// Initialize the state
	st, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	err = st.Initialize(censusRoot, ballotMode, encryptionKeys)
	c.Assert(err, qt.IsNil)

	// Generate a batch of random votes
	err = st.StartBatch()
	c.Assert(err, qt.IsNil)
	for i := range nVotes {
		err = st.AddVote(newMockVote(pubKey, i, 100))
		c.Assert(err, qt.IsNil)
	}
	err = st.EndBatch()
	c.Assert(err, qt.IsNil)

	// Get encrypted votes
	encryptedAddAccumulator, addOk := st.ResultsAdd()
	c.Assert(addOk, qt.IsTrue, qt.Commentf("Add accumulator should be available"))
	encryptedSubAccumulator, subOk := st.ResultsSub()
	c.Assert(subOk, qt.IsTrue, qt.Commentf("Sub accumulator should be available"))

	// Decrypt the votes and generate the decryption proofs
	maxValue := ballotMode.MaxValue.Uint64() * 1000
	addAccumulator := [types.FieldsPerBallot]*big.Int{}
	addCiphertexts := [types.FieldsPerBallot]elgamal.Ciphertext{}
	addDecryptionProofs := [types.FieldsPerBallot]*elgamal.DecryptionProof{}
	for i, ct := range encryptedAddAccumulator.Ciphertexts {
		c.Assert(ct.C1 != nil && ct.C2 != nil, qt.IsTrue)
		addCiphertexts[i] = *ct
		_, result, err := elgamal.Decrypt(pubKey, privKey, ct.C1, ct.C2, maxValue)
		c.Assert(err, qt.IsNil)
		addAccumulator[i] = result
		addDecryptionProofs[i], err = elgamal.BuildDecryptionProof(privKey, pubKey, ct.C1, ct.C2, result)
		c.Assert(err, qt.IsNil)
	}
	resultsAccumulator := [types.FieldsPerBallot]*big.Int{}
	subAccumulator := [types.FieldsPerBallot]*big.Int{}
	subCiphertexts := [types.FieldsPerBallot]elgamal.Ciphertext{}
	subDecryptionProofs := [types.FieldsPerBallot]*elgamal.DecryptionProof{}
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

	// Generete the witness for the circuit
	witness, err := GenerateWitness(
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

	// Log the time to generate the witness
	c.Logf("inputs generation took %s", time.Since(now).String())

	// Start the proving process
	now = time.Now()
	assert := test.NewAssert(t)
	assert.SolvingSucceeded(&ResultsVerifierCircuit{}, witness,
		test.WithCurves(circuits.ResultsVerifierCurve), test.WithBackends(backend.GROTH16))
	c.Logf("proving took %s", time.Since(now).String())
}

func newMockVote(pubKey ecc.Point, index, amount int) *state.Vote {
	fields := [types.FieldsPerBallot]*big.Int{}
	for i := range fields {
		fields[i] = big.NewInt(int64(amount + i))
	}
	ballot, err := elgamal.NewBallot(bjj.New()).Encrypt(fields, pubKey, nil)
	if err != nil {
		panic(fmt.Errorf("error encrypting: %v", err))
	}
	return &state.Vote{
		// This circuit does not use the ballot, so we can create it empty,
		// only to prevent nil pointer dereference. The value that matters is
		// the ReencryptedBallot.
		Ballot:            elgamal.NewBallot(state.Curve),
		ReencryptedBallot: ballot,
		VoteID:            util.RandomBytes(20),
		Address:           big.NewInt(int64(index + 200)), // mock
	}
}
