package results

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/vocdoni-z-sandbox/circuits/merkleproof"
	"github.com/vocdoni/vocdoni-z-sandbox/crypto/elgamal"
	"github.com/vocdoni/vocdoni-z-sandbox/state"
	"github.com/vocdoni/vocdoni-z-sandbox/types"
)

func GenerateWitness(
	o *state.State,
	results [types.FieldsPerBallot]*big.Int,
	addResults [types.FieldsPerBallot]*big.Int,
	subResults [types.FieldsPerBallot]*big.Int,
	addCiphertexts [types.FieldsPerBallot]elgamal.Ciphertext,
	subCiphertexts [types.FieldsPerBallot]elgamal.Ciphertext,
	decryptionAddProofs [types.FieldsPerBallot]*elgamal.DecryptionProof,
	decryptionSubProofs [types.FieldsPerBallot]*elgamal.DecryptionProof,
) (*ResultsVerifierCircuit, error) {
	var err error
	witness := &ResultsVerifierCircuit{}

	// State root hash
	witness.StateRoot, err = o.RootAsBigInt()
	if err != nil {
		return nil, fmt.Errorf("failed to get state root hash: %w", err)
	}

	// Encrypted and decrypted results
	for i := range types.FieldsPerBallot {
		witness.AddCiphertexts[i] = *addCiphertexts[i].ToGnark()
		witness.SubCiphertexts[i] = *subCiphertexts[i].ToGnark()
		witness.Results[i] = results[i]
		witness.ResultsAdd[i] = addResults[i]
		witness.ResultsSub[i] = subResults[i]
	}

	// Results accumulators proofs
	witness.ResultsAddProof, err = merkleProofFromKey(o, state.KeyResultsAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to transform results add arbo proof to merkle proof: %w", err)
	}
	witness.ResultsSubProof, err = merkleProofFromKey(o, state.KeyResultsSub)
	if err != nil {
		return nil, fmt.Errorf("failed to transform results sub arbo proof to merkle proof: %w", err)
	}

	// Decryption add and sub proofs
	for i := range types.FieldsPerBallot {
		witness.DecryptionAddProofs[i] = decryptionAddProofs[i].ToGnark()
		witness.DecryptionSubProofs[i] = decryptionSubProofs[i].ToGnark()
	}

	// EncryptionKey proof and public key
	witness.EncryptionKeyProof, err = merkleProofFromKey(o, state.KeyEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to transform encryption key arbo proof to merkle proof: %w", err)
	}
	witness.EncryptionPublicKey.PubKey = [2]frontend.Variable{
		o.Process().EncryptionKey.PubKey[0],
		o.Process().EncryptionKey.PubKey[1],
	}

	return witness, nil
}

func merkleProofFromKey(o *state.State, key *big.Int) (merkleproof.MerkleProof, error) {
	proof, err := o.GenArboProof(key)
	if err != nil {
		return merkleproof.MerkleProof{}, fmt.Errorf("failed to generate arbo proof: %w", err)
	}
	return merkleproof.MerkleProofFromArboProof(proof)
}
