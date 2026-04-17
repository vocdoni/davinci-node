package results

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/davinci-node/circuits/merkleproof"
	"github.com/vocdoni/davinci-node/crypto/elgamal"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/state"
	"github.com/vocdoni/davinci-node/types"
)

// GenerateAssignment builds the circuit assignment for the results verifier.
func GenerateAssignment(
	o *state.State,
	results [params.FieldsPerBallot]*big.Int,
	accumulators [params.FieldsPerBallot]*big.Int,
	accumulatorsEncrypted [params.FieldsPerBallot]elgamal.Ciphertext,
	decryptionProofs [params.FieldsPerBallot]*elgamal.DecryptionProof,
) (*ResultsVerifierCircuit, error) {
	var err error
	assignment := &ResultsVerifierCircuit{}

	// State root hash
	assignment.StateRoot, err = o.RootAsBigInt()
	if err != nil {
		return nil, fmt.Errorf("failed to get state root hash: %w", err)
	}

	// Encrypted and decrypted results
	for i := range params.FieldsPerBallot {
		assignment.AccumulatorsEncrypted[i] = *accumulatorsEncrypted[i].ToGnark()
		assignment.Results[i] = results[i]
		assignment.Accumulators[i] = accumulators[i]
	}

	// Results accumulators proof
	assignment.AccumulatorsMerkleProof, err = merkleProofFromKey(o, state.KeyResults)
	if err != nil {
		return nil, fmt.Errorf("failed to transform results arbo proof to merkle proof: %w", err)
	}

	// Decryption proofs
	for i := range params.FieldsPerBallot {
		assignment.DecryptionProofs[i] = decryptionProofs[i].ToGnark()
	}

	// EncryptionKey proof and public key
	assignment.EncryptionKeyMerkleProof, err = merkleProofFromKey(o, state.KeyEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to transform encryption key arbo proof to merkle proof: %w", err)
	}
	assignment.EncryptionPublicKey.PubKey = [2]frontend.Variable{
		o.Process().EncryptionKey.PubKey[0],
		o.Process().EncryptionKey.PubKey[1],
	}

	return assignment, nil
}

func merkleProofFromKey(o *state.State, key types.StateKey) (merkleproof.MerkleProof, error) {
	proof, err := o.GenArboProof(key)
	if err != nil {
		return merkleproof.MerkleProof{}, fmt.Errorf("failed to generate arbo proof: %w", err)
	}
	return merkleproof.MerkleProofFromArboProof(proof)
}
