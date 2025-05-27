package results

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/merkleproof"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// HashFn is the hash function used in the circuit. It should the equivalent
// hash function used in the state package (state.HashFn).
var HashFn = poseidon.MultiHash

type ResultsVerifierCircuit struct {
	StateRoot                  frontend.Variable                        `gnark:",public"`
	Results                    [types.FieldsPerBallot]frontend.Variable `gnark:",public"`
	AddAccumulators            [types.FieldsPerBallot]frontend.Variable
	SubAccumulators            [types.FieldsPerBallot]frontend.Variable
	AddAccumulatorsEncrypted   circuits.Ballot
	SubAccumulatorsEncrypted   circuits.Ballot
	AddAccumulatorsMerkleProof merkleproof.MerkleProof
	SubAccumulatorsMerkleProof merkleproof.MerkleProof
	DecryptionAddProofs        [types.FieldsPerBallot]elgamal.DecryptionProof
	DecryptionSubProofs        [types.FieldsPerBallot]elgamal.DecryptionProof
	EncryptionKeyMerkleProof   merkleproof.MerkleProof
	EncryptionPublicKey        circuits.EncryptionKey[frontend.Variable]
}

func (c *ResultsVerifierCircuit) Define(api frontend.API) error {
	// Verify that the accumulators values matches with the proofs values
	c.VerifyAccumulatorsHashes(api)
	// Verify results add, results sub, and encryption key proofs
	c.VerifyMerkleProofs(api)
	// Verify decryption proofs for add and sub ciphertexts
	c.VerifyDecryptionProofs(api)
	// Verify that the results provided match with the substraction of the
	// add results and the sub results
	c.VerifyResults(api)
	return nil
}

func (c *ResultsVerifierCircuit) VerifyAccumulatorsHashes(api frontend.API) {
	// Compute the value of the add ciphertexts in the merkle tree
	addMerkletreeValue, err := HashFn(api, c.AddAccumulatorsEncrypted.SerializeVars()...)
	if err != nil {
		circuits.FrontendError(api, "failed to hash add ciphertexts", err)
		return
	}
	// Compute the hash of the leaf in the merkle tree
	addLeafHash, err := HashFn(api, state.KeyResultsAdd, addMerkletreeValue, 1)
	if err != nil {
		circuits.FrontendError(api, "failed to hash add leaf", err)
		return
	}
	// Check that the computed leaf hash matches the one in the proof
	api.AssertIsEqual(addLeafHash, c.AddAccumulatorsMerkleProof.LeafHash)

	// Compute the value of the sub ciphertexts in the merkle tree
	subMerkletreeValue, err := HashFn(api, c.SubAccumulatorsEncrypted.SerializeVars()...)
	if err != nil {
		circuits.FrontendError(api, "failed to hash sub ciphertexts", err)
		return
	}
	// Compute the hash of the leaf in the merkle tree
	subLeafHash, err := HashFn(api, state.KeyResultsSub, subMerkletreeValue, 1)
	if err != nil {
		circuits.FrontendError(api, "failed to hash sub leaf", err)
		return
	}
	// Check that the computed leaf hash matches the one in the proof
	api.AssertIsEqual(subLeafHash, c.SubAccumulatorsMerkleProof.LeafHash)
}

func (c *ResultsVerifierCircuit) VerifyMerkleProofs(api frontend.API) {
	// Verify the results add proof
	c.AddAccumulatorsMerkleProof.Verify(api, HashFn, c.StateRoot)
	// Verify the results sub proof
	c.SubAccumulatorsMerkleProof.Verify(api, HashFn, c.StateRoot)
	// Verify the encryption key proof
	c.EncryptionKeyMerkleProof.Verify(api, HashFn, c.StateRoot)
}

func (c *ResultsVerifierCircuit) VerifyDecryptionProofs(api frontend.API) {
	pubKey := twistededwards.Point{
		X: c.EncryptionPublicKey.PubKey[0],
		Y: c.EncryptionPublicKey.PubKey[1],
	}
	for i := range types.FieldsPerBallot {
		// Prove the decryption add proofs
		err := c.DecryptionAddProofs[i].Verify(api, HashFn, pubKey, c.AddAccumulatorsEncrypted[i], c.AddAccumulators[i])
		if err != nil {
			circuits.FrontendError(api, "failed to verify add decryption proof", err)
			return
		}
		// Prove the decryption sub proofs
		err = c.DecryptionSubProofs[i].Verify(api, HashFn, pubKey, c.SubAccumulatorsEncrypted[i], c.SubAccumulators[i])
		if err != nil {
			circuits.FrontendError(api, "failed to verify sub decryption proof", err)
			return
		}
	}
}

func (c *ResultsVerifierCircuit) VerifyResults(api frontend.API) {
	// Verify that the results add minus results sub equals results
	for i := range types.FieldsPerBallot {
		api.AssertIsEqual(
			api.Sub(c.AddAccumulators[i], c.SubAccumulators[i]),
			c.Results[i],
		)
	}
}
