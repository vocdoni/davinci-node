package results

import (
	"errors"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	bjj "github.com/vocdoni/davinci-node/crypto/ecc/bjj_gnark"
	"github.com/vocdoni/davinci-node/crypto/ecc/curves"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
	"github.com/vocdoni/gnark-crypto-primitives/hash/native/bn254/poseidon"
)

// HashFn is the hash function used in the circuit. It should the equivalent
// hash function used in the state package (state.HashFn).
var HashFn = poseidon.MultiHash

type ResultsVerifierCircuit struct {
	StateRoot                frontend.Variable                         `gnark:",public"`
	Results                  [params.FieldsPerBallot]frontend.Variable `gnark:",public"`
	Accumulators             [params.FieldsPerBallot]frontend.Variable
	AccumulatorsEncrypted    circuits.Ballot
	AccumulatorsMerkleProof  merkleproof.MerkleProof
	DecryptionProofs         [params.FieldsPerBallot]elgamal.DecryptionProof
	EncryptionKeyMerkleProof merkleproof.MerkleProof
	EncryptionPublicKey      circuits.EncryptionKey[frontend.Variable]
}

func (c *ResultsVerifierCircuit) Define(api frontend.API) error {
	c.forceCommitment(api)
	// Verify results and encryption key proofs
	c.VerifyMerkleProofs(api)
	// Verify that the Merkle proof leaf hashes match the witness values
	c.VerifyMerkleProofLeaves(api)
	// Verify decryption proofs for the accumulator ciphertexts
	c.VerifyDecryptionProofs(api)
	// Verify that the decrypted accumulator matches the public results.
	c.VerifyResults(api)
	return nil
}

func (c *ResultsVerifierCircuit) forceCommitment(api frontend.API) {
	cmter, ok := api.(frontend.Committer)
	if !ok {
		circuits.FrontendError(api, "circuit must implement frontend.Committer", errors.New("circuit does not implement frontend.Committer"))
		return
	}
	res, err := cmter.Commit(c.EncryptionPublicKey.Serialize()...)
	if err != nil {
		circuits.FrontendError(api, "failed to commit encryption public key", err)
		return
	}
	api.AssertIsDifferent(res, 0)
}

func (c *ResultsVerifierCircuit) VerifyMerkleProofs(api frontend.API) {
	// Verify the results proof
	c.AccumulatorsMerkleProof.Verify(api, HashFn, c.StateRoot)
	// Verify the encryption key proof
	c.EncryptionKeyMerkleProof.Verify(api, HashFn, c.StateRoot)
}

func (c *ResultsVerifierCircuit) VerifyMerkleProofLeaves(api frontend.API) {
	api.AssertIsEqual(c.AccumulatorsMerkleProof.Key, state.KeyResults.ToGnark())
	api.AssertIsEqual(c.EncryptionKeyMerkleProof.Key, state.KeyEncryptionKey.ToGnark())

	if err := c.EncryptionKeyMerkleProof.VerifyLeafHash(api, HashFn, c.EncryptionPublicKey.Serialize()...); err != nil {
		circuits.FrontendError(api, "failed to verify encryption key proof leaf hash", err)
		return
	}
	if err := c.AccumulatorsMerkleProof.VerifyLeafHash(api, HashFn, c.AccumulatorsEncrypted.SerializeVars()...); err != nil {
		circuits.FrontendError(api, "failed to verify accumulators proof leaf hash", err)
		return
	}
}

func (c *ResultsVerifierCircuit) VerifyDecryptionProofs(api frontend.API) {
	pubKey := twistededwards.Point{
		X: c.EncryptionPublicKey.PubKey[0],
		Y: c.EncryptionPublicKey.PubKey[1],
	}
	for i := range params.FieldsPerBallot {
		err := c.DecryptionProofs[i].Verify(api, HashFn, pubKey, c.AccumulatorsEncrypted[i], c.Accumulators[i])
		if err != nil {
			circuits.FrontendError(api, "failed to verify decryption proof", err)
			return
		}
	}
}

func (c *ResultsVerifierCircuit) VerifyResults(api frontend.API) {
	bjjOrderMinusOne := new(big.Int).Sub(curves.New(bjj.CurveType).Order(), big.NewInt(1))

	// Verify that the decrypted accumulator matches the public results
	for i := range params.FieldsPerBallot {
		api.AssertIsLessOrEqual(c.Accumulators[i], bjjOrderMinusOne)
		api.AssertIsEqual(c.Accumulators[i], c.Results[i])
	}
}
