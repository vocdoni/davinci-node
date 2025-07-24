package recursion

import "fmt"

// BallotProofNPubInputs is the number of public inputs for the ballot proof
// circom circuit.
const BallotProofNPubInputs = 1

// Circom2GnarkProof function is a wrapper to convert a circom proof to a gnark
// proof, it receives the circom proof and the public signals as strings, as
// snarkjs returns them. Then, it parses the inputs to the gnark format. It
// returns a CircomProof and a list of public signals or an error.
func Circom2GnarkProof(circomProof, pubSignals string) (*CircomProof, []string, error) {
	// transform to gnark format
	proofData, err := UnmarshalCircomProofJSON([]byte(circomProof))
	if err != nil {
		return nil, nil, err
	}
	pubSignalsData, err := UnmarshalCircomPublicSignalsJSON([]byte(pubSignals))
	if err != nil {
		return nil, nil, err
	}
	return proofData, pubSignalsData, nil
}

// Circom2GnarkProofForRecursion function is a wrapper to convert a circom
// proof to a gnark proof to be verified inside another gnark circuit. It
// receives the circom proof, the public signals and the verification key as
// strings, as snarkjs returns them. Then, it converts the proof, the public
// signals and the verification key to the gnark format and returns a gnark
// recursion proof or an error.
func Circom2GnarkProofForRecursion(vkey []byte, circomProof, pubSignals string) (*GnarkRecursionProof, error) {
	// transform to gnark format
	gnarkProofData, gnarkPubSignalsData, err := Circom2GnarkProof(circomProof, pubSignals)
	if err != nil {
		return nil, err
	}
	gnarkVKeyData, err := UnmarshalCircomVerificationKeyJSON(vkey)
	if err != nil {
		return nil, err
	}
	proof, err := ConvertCircomToGnarkRecursion(gnarkVKeyData, gnarkProofData, gnarkPubSignalsData, true)
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
	if ok, err := VerifyProof(gnarkProof); !ok || err != nil {
		return nil, fmt.Errorf("proof verification failed: %v", err)
	}
	return ConvertCircomToGnarkRecursion(gnarkVKeyData, proof, pubSignals, true)
}

// Circom2GnarkPlaceholder function is a wrapper to convert the circom ballot
// circuit to a gnark recursion placeholder, it returns the resulting
// placeholders for the
func Circom2GnarkPlaceholder(vkey []byte) (*GnarkRecursionPlaceholders, error) {
	gnarkVKeyData, err := UnmarshalCircomVerificationKeyJSON(vkey)
	if err != nil {
		return nil, err
	}
	return PlaceholdersForRecursion(gnarkVKeyData, BallotProofNPubInputs, true)
}
