package format

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/std/math/emulated"
	"github.com/consensys/gnark/std/math/emulated/emparams"
)

// SplitIntoEmulatedLimbsLE returns gnark-emulated limbs (little-endian) for z in field T.
// nativeField is the circuit's native scalar field modulus (e.g. ecc.BN254.ScalarField()).
func SplitIntoEmulatedLimbsLE[T emulated.FieldParams](nativeField *big.Int, z *big.Int) ([]*big.Int, error) {
	if z == nil {
		return nil, fmt.Errorf("nil input z")
	}

	// Stage the witness value (decomposition deferred by ValueOf)
	e := emulated.ValueOf[T](z)

	// Force gnark to allocate limbs now
	e.Initialize(nativeField)
	if len(e.Limbs) == 0 {
		return nil, fmt.Errorf("no limbs allocated")
	}

	// Extract limbs (LE). For host-side witnesses, variables are *big.Int.
	out := make([]*big.Int, len(e.Limbs))
	for i, v := range e.Limbs {
		bi, ok := v.(*big.Int)
		if !ok {
			return nil, fmt.Errorf("unexpected limb type %T", v)
		}
		out[i] = new(big.Int).Set(bi)
	}

	return out, nil
}

// SplitYForBn254FromBLS12381 splits a BLS12-381 scalar field element z into limbs for emulation in a BN254 circuit.
// It returns the limbs as []*big.Int in little-endian order.
// The returned limbs can be used to reconstruct z within a gnark circuit emulating BLS12-381 Fr.
func SplitYForBn254FromBLS12381(z *big.Int) ([]*big.Int, error) {
	limbs, err := SplitIntoEmulatedLimbsLE[emparams.BLS12381Fr](ecc.BN254.ScalarField(), z)
	if err != nil {
		return nil, fmt.Errorf("failed to split BLS12-381 Fr element into limbs: %w", err)
	}
	if len(limbs) != 4 {
		return nil, fmt.Errorf("expected 4 limbs for BLS12-381 Fr element, got %d", len(limbs))
	}
	return limbs, nil
}
