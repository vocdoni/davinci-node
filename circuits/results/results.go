package results

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/algebra/native/twistededwards"
	"github.com/vocdoni/gnark-crypto-primitives/elgamal"
	"github.com/vocdoni/gnark-crypto-primitives/hash/bn254/poseidon"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/merkleproof"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

// HashFn is the hash function used in the circuit. It should the equivalent
// hash function used in the state package (state.HashFn).
var HashFn = poseidon.MultiHash

type ResultsVerifierCircuit struct {
	StateRoot           frontend.Variable
	ResultsAdd          [types.FieldsPerBallot]frontend.Variable
	ResultsSub          [types.FieldsPerBallot]frontend.Variable
	AddCiphertexts      [types.FieldsPerBallot]elgamal.Ciphertext
	SubCiphertexts      [types.FieldsPerBallot]elgamal.Ciphertext
	ResultsAddProof     merkleproof.MerkleProof
	ResultsSubProof     merkleproof.MerkleProof
	DecryptionAddProofs [types.FieldsPerBallot]elgamal.DecryptionProof
	DecryptionSubProofs [types.FieldsPerBallot]elgamal.DecryptionProof
	EncryptionKeyProof  merkleproof.MerkleProof
	EncryptionPublicKey circuits.EncryptionKey[frontend.Variable]
}

func (c *ResultsVerifierCircuit) Define(api frontend.API) error {
	// Prove the resulsts add
	c.ResultsAddProof.Verify(api, HashFn, c.StateRoot)
	// Prove the results sub
	c.ResultsSubProof.Verify(api, HashFn, c.StateRoot)
	// Prove the encryption key
	c.EncryptionKeyProof.Verify(api, HashFn, c.StateRoot)

	pubKey := twistededwards.Point{
		X: c.EncryptionPublicKey.PubKey[0],
		Y: c.EncryptionPublicKey.PubKey[1],
	}

	for i := range types.FieldsPerBallot {
		// Prove the decryption add proofs
		err := c.DecryptionAddProofs[i].Verify(api, HashFn, pubKey, c.AddCiphertexts[i], c.ResultsAdd[i])
		if err != nil {
			return err
		}
		// // Prove the decryption sub proofs
		// err = c.DecryptionSubProofs[i].Verify(api, HashFn, pubKey, c.SubCiphertexts[i], c.ResultsSub[i])
		// if err != nil {
		// 	return err
		// }
	}

	return nil
}
