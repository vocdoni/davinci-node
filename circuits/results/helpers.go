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

func GenerateWitness(
	o *state.State,
	results [params.FieldsPerBallot]*big.Int,
	addAccumulators [params.FieldsPerBallot]*big.Int,
	subAccumulators [params.FieldsPerBallot]*big.Int,
	addAccumulatorsEncrypted [params.FieldsPerBallot]elgamal.Ciphertext,
	subAccumulatorsEncrypted [params.FieldsPerBallot]elgamal.Ciphertext,
	decryptionAddProofs [params.FieldsPerBallot]*elgamal.DecryptionProof,
	decryptionSubProofs [params.FieldsPerBallot]*elgamal.DecryptionProof,
) (*ResultsVerifierCircuit, error) {
	var err error
	witness := &ResultsVerifierCircuit{}

	// State root hash
	witness.StateRoot, err = o.RootAsBigInt()
	if err != nil {
		return nil, fmt.Errorf("failed to get state root hash: %w", err)
	}

	// Encrypted and decrypted results
	for i := range params.FieldsPerBallot {
		witness.AddAccumulatorsEncrypted[i] = *addAccumulatorsEncrypted[i].ToGnark()
		witness.SubAccumulatorsEncrypted[i] = *subAccumulatorsEncrypted[i].ToGnark()
		witness.Results[i] = results[i]
		witness.AddAccumulators[i] = addAccumulators[i]
		witness.SubAccumulators[i] = subAccumulators[i]
	}

	// Results accumulators proofs
	witness.AddAccumulatorsMerkleProof, err = merkleProofFromKey(o, state.KeyResultsAdd)
	if err != nil {
		return nil, fmt.Errorf("failed to transform results add arbo proof to merkle proof: %w", err)
	}
	witness.SubAccumulatorsMerkleProof, err = merkleProofFromKey(o, state.KeyResultsSub)
	if err != nil {
		return nil, fmt.Errorf("failed to transform results sub arbo proof to merkle proof: %w", err)
	}

	// Decryption add and sub proofs
	for i := range params.FieldsPerBallot {
		witness.DecryptionAddProofs[i] = decryptionAddProofs[i].ToGnark()
		witness.DecryptionSubProofs[i] = decryptionSubProofs[i].ToGnark()
	}

	// EncryptionKey proof and public key
	witness.EncryptionKeyMerkleProof, err = merkleProofFromKey(o, state.KeyEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to transform encryption key arbo proof to merkle proof: %w", err)
	}
	witness.EncryptionPublicKey.PubKey = [2]frontend.Variable{
		o.Process().EncryptionKey.PubKey[0],
		o.Process().EncryptionKey.PubKey[1],
	}

	return witness, nil
}

func merkleProofFromKey(o *state.State, key types.StateKey) (merkleproof.MerkleProof, error) {
	proof, err := o.GenArboProof(key)
	if err != nil {
		return merkleproof.MerkleProof{}, fmt.Errorf("failed to generate arbo proof: %w", err)
	}
	return merkleproof.MerkleProofFromArboProof(proof)
}
