package results

import (
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
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/internal/testutil"
	"github.com/vocdoni/davinci-node/state"
	statetest "github.com/vocdoni/davinci-node/state/testutil"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/types/params"
)

const nVotes = 10

func TestResultsVerifierCircuit(t *testing.T) {
	c := qt.New(t)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	logger.Set(zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).With().Timestamp().Logger())

	// Start the test
	now := time.Now()

	// Random inputs for the state (processID and censusRoot)
	processID := testutil.RandomProcessID()
	censusOrigin := types.CensusOriginMerkleTreeOffchainStaticV1
	ballotMode := testutil.BallotMode()

	// Generate a random ElGamal key pair
	pubKey, privKey, err := elgamal.GenerateKey(curves.New(bjj.CurveType))
	c.Assert(err, qt.IsNil)
	encryptionKeys := circuits.EncryptionKeyFromECCPoint(pubKey)

	// Initialize the state
	st, err := state.New(memdb.New(), processID)
	c.Assert(err, qt.IsNil)
	err = st.Initialize(censusOrigin.BigInt().MathBigInt(), ballotMode, encryptionKeys)
	c.Assert(err, qt.IsNil)

	// Generate a batch of random votes
	err = st.StartBatch()
	c.Assert(err, qt.IsNil)
	for i := range nVotes {
		err = st.AddVote(statetest.NewVoteForTest(pubKey, uint64(i), 100))
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
		test.WithCurves(params.ResultsVerifierCurve), test.WithBackends(backend.GROTH16))
	c.Logf("proving took %s", time.Since(now).String())
}
