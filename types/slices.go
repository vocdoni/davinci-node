package types

import "math/big"

// SliceOf converts a slice of type F to a slice of type T using the provided
// conversion function. It returns a new slice of type T with the converted
// values.
func SliceOf[F, T any](from []F, conv func(F) T) []T {
	to := make([]T, len(from))
	for i, v := range from {
		to[i] = conv(v)
	}
	return to
}

// BigIntConverter converts a *big.Int to a *BigInt. It returns a new *BigInt
// with the value set to the value of the provided *big.Int. It can be used as
// a conversion function for SliceOf to convert a slice of *big.Int to a slice
// of *BigInt.
func BigIntConverter(from *big.Int) *BigInt {
	return new(BigInt).SetBigInt(from)
}
