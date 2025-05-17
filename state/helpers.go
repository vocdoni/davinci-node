package state

import (
	"math/big"

	"github.com/vocdoni/arbo"
)

// BytesToBigInt method converts a byte array to a big.Int. It is a wrapper
// around the arbo.BytesToBigInt function.
func BytesToBigInt(b []byte) *big.Int {
	return arbo.BytesToBigInt(b)
}

// BigIntToBytes method converts a big.Int to a byte array. It is a wrapper
// around the arbo.BigIntToBytes function, which uses the maximum key length
// for the hash function.
func BigIntToBytes(b *big.Int) []byte {
	return arbo.BigIntToBytes(HashFn.Len(), b)
}
