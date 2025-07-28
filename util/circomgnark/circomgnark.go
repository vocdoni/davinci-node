// circomgnark package provides utilities to convert between Circom and Gnark
// proof formats, allowing for verification of zkSNARK proofs created with
// Circom and SnarkJS to be used within the Gnark framework.
// It includes functions to convert Circom proofs and verification keys to
// Gnark over the BN254 curve, and to verify these proofs using Gnark's
// verification functions. It also provides a way to handle recursive proofs
// and placeholders for recursive circuits.
package circomgnark

import "fmt"

// Circom2GnarkProofForRecursion function is a wrapper to convert a circom
// proof to a gnark proof to be verified inside another gnark circuit. It
// receives the circom proof, the public signals and the verification key as
// strings, as snarkjs returns them. Then, it converts the proof, the public
// signals and the verification key to the gnark format and returns a gnark
// recursion proof or an error.
func Circom2GnarkProofForRecursion(vkey []byte, rawCircomProof, rawPubSignals string) (*GnarkRecursionProof, error) {
	// transform to gnark format
	circomProof, circomPubSignals, err := UnmarshalCircom(rawCircomProof, rawPubSignals)
	if err != nil {
		return nil, err
	}
	circomVerificationKey, err := UnmarshalCircomVerificationKeyJSON(vkey)
	if err != nil {
		return nil, err
	}
	proof, err := circomProof.ToGnarkRecursion(circomVerificationKey, circomPubSignals, true)
	if err != nil {
		return nil, err
	}
	return proof, nil
}

// VerifyAndConvertToRecursion function is a wrapper to circom2gnark that
// converts a circom proof to a gnark proof, verifies it and then converts it
// to a gnark recursion proof. It returns the resulting proof or an error.
func VerifyAndConvertToRecursion(vkey []byte, proof *CircomProof, pubSignals []string) (
	*GnarkRecursionProof, error,
) {
	gnarkVKeyData, err := UnmarshalCircomVerificationKeyJSON(vkey)
	if err != nil {
		return nil, err
	}
	gnarkProof, err := ConvertCircomToGnark(gnarkVKeyData, proof, pubSignals)
	if err != nil {
		return nil, err
	}
	if ok, err := gnarkProof.Verify(); !ok || err != nil {
		return nil, fmt.Errorf("proof verification failed: %v", err)
	}
	return proof.ToGnarkRecursion(gnarkVKeyData, pubSignals, true)
}

// Circom2GnarkPlaceholder function is a wrapper to convert the circom ballot
// circuit to a gnark recursion placeholder, it returns the resulting
// placeholders for the
func Circom2GnarkPlaceholder(vkey []byte, nInputs int) (*GnarkRecursionPlaceholders, error) {
	gnarkVKeyData, err := UnmarshalCircomVerificationKeyJSON(vkey)
	if err != nil {
		return nil, err
	}
	return PlaceholdersForRecursion(gnarkVKeyData, nInputs, true)
}
