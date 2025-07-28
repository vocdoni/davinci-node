package circomgnark

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/std/math/emulated"
	recursion "github.com/consensys/gnark/std/recursion/groth16"

	groth16_bn254 "github.com/consensys/gnark/backend/groth16/bn254"
	"github.com/consensys/gnark/std/algebra/emulated/sw_bn254"
)

// ToGnarkRecursion converts a Circom proof, verification key, and public
// signals to the Gnark recursion proof format. If fixedVk is true, the
// verification key is fixed and must be defined as 'gnark:"-"' in the
// Circuit.
func (circomProof *CircomProof) ToGnarkRecursion(circomVk *CircomVerificationKey,
	circomPublicSignals []string, fixedVk bool,
) (*GnarkRecursionProof, error) {
	// Convert public signals to field elements
	publicInputs, err := ConvertPublicInputs(circomPublicSignals)
	if err != nil {
		return nil, err
	}
	// Convert the proof and verification key to gnark types
	gnarkProof, err := circomProof.ToGnark()
	if err != nil {
		return nil, err
	}
	// Convert the proof and verification key to recursion types
	recursionProof, err := recursion.ValueOfProof[sw_bn254.G1Affine, sw_bn254.G2Affine](gnarkProof)
	if err != nil {
		return nil, fmt.Errorf("failed to convert proof to recursion proof: %w", err)
	}
	// Force initialization of all G1 and G2 elements
	recursionProof.Ar.X.Initialize(ecc.BN254.BaseField())
	recursionProof.Ar.Y.Initialize(ecc.BN254.BaseField())
	recursionProof.Bs.P.X.A0.Initialize(ecc.BN254.BaseField())
	recursionProof.Bs.P.X.A1.Initialize(ecc.BN254.BaseField())
	recursionProof.Bs.P.Y.A0.Initialize(ecc.BN254.BaseField())
	recursionProof.Bs.P.Y.A1.Initialize(ecc.BN254.BaseField())
	recursionProof.Krs.X.Initialize(ecc.BN254.BaseField())
	recursionProof.Krs.Y.Initialize(ecc.BN254.BaseField())
	// Convert the verification key to recursion verification key
	gnarkVk, err := circomVk.ToGnark()
	if err != nil {
		return nil, err
	}
	// Transform the public inputs to emulated elements for the recursion circuit
	publicInputElementsEmulated := make([]emulated.Element[sw_bn254.ScalarField], len(publicInputs))
	for i, input := range publicInputs {
		bigIntValue := input.BigInt(new(big.Int))
		elem := emulated.ValueOf[sw_bn254.ScalarField](bigIntValue)
		publicInputElementsEmulated[i] = elem
	}
	// Create assignments
	assignments := &GnarkRecursionProof{
		Proof: recursionProof,
		PublicInputs: recursion.Witness[sw_bn254.ScalarField]{
			Public: publicInputElementsEmulated,
		},
	}
	if !fixedVk {
		// Create the recursion types
		recursionVk, err := recursion.ValueOfVerifyingKey[sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl](gnarkVk)
		if err != nil {
			return nil, fmt.Errorf("failed to convert verification key to recursion verification key: %w", err)
		}
		assignments.Vk = recursionVk
	}
	return assignments, nil
}

// PlaceholdersForRecursion creates placeholders for the recursion proof and
// verification key. If fixedVk is true, the verification key is fixed and must
// be defined as 'gnark:"-"' in the Circuit. It only needs the number of public
// inputs and the circom verification key.
func PlaceholdersForRecursion(circomVk *CircomVerificationKey,
	nPublicInputs int, fixedVk bool,
) (*GnarkRecursionPlaceholders, error) {
	// convert the verification key to recursion verification key
	gnarkVk, err := circomVk.ToGnark()
	if err != nil {
		return nil, err
	}
	// create the placeholder for the recursion circuit
	if fixedVk {
		return createPlaceholdersForRecursionWithFixedVk(gnarkVk, nPublicInputs)
	}
	return createPlaceholdersForRecursion(gnarkVk, nPublicInputs)
}

// createPlaceholdersForRecursion creates placeholders for the recursion proof
// and verification key. It returns a set of placeholders needed to define the
// recursive circuit. Use this function when the verification key is fixed
// (defined as 'gnark:"-"').
func createPlaceholdersForRecursionWithFixedVk(gnarkVk *groth16_bn254.VerifyingKey,
	nPublicInputs int,
) (*GnarkRecursionPlaceholders, error) {
	if gnarkVk == nil || nPublicInputs < 0 {
		return nil, fmt.Errorf("invalid inputs to create placeholders for recursion")
	}
	placeholderVk, err := recursion.ValueOfVerifyingKeyFixed[sw_bn254.G1Affine, sw_bn254.G2Affine, sw_bn254.GTEl](gnarkVk)
	if err != nil {
		return nil, fmt.Errorf("failed to convert verification key to recursion verification key: %w", err)
	}

	placeholderWitness := recursion.Witness[sw_bn254.ScalarField]{
		Public: make([]emulated.Element[sw_bn254.ScalarField], nPublicInputs),
	}
	placeholderProof := recursion.Proof[sw_bn254.G1Affine, sw_bn254.G2Affine]{}

	return &GnarkRecursionPlaceholders{
		Vk:      placeholderVk,
		Witness: placeholderWitness,
		Proof:   placeholderProof,
	}, nil
}

// createPlaceholdersForRecursion creates placeholders for the recursion proof
// and verification key. It returns a set of placeholders needed to define the
// recursive circuit. Use this function when the verification key is not fixed.
func createPlaceholdersForRecursion(gnarkVk *groth16_bn254.VerifyingKey,
	nPublicInputs int,
) (*GnarkRecursionPlaceholders, error) {
	placeholders, err := createPlaceholdersForRecursionWithFixedVk(gnarkVk, nPublicInputs)
	if err != nil {
		return nil, err
	}
	placeholders.Vk.G1.K = make([]sw_bn254.G1Affine, len(placeholders.Vk.G1.K))
	return placeholders, nil
}
