package util

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"reflect"

	"github.com/consensys/gnark/frontend"
	"github.com/vocdoni/arbo"
)

// RandomBytes generates a random byte slice of length n.
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	return b
}

// Random32 generates a random 32-byte array.
func Random32() [32]byte {
	var bytes [32]byte
	copy(bytes[:], RandomBytes(32))
	return bytes
}

// RandomHex generates a random hex string of length n.
func RandomHex(n int) string {
	return fmt.Sprintf("%x", RandomBytes(n))
}

// RandomBigInt generates a random big integer between min and max.
func RandomBigInt(min, max *big.Int) *big.Int {
	num, err := rand.Int(rand.Reader, new(big.Int).Sub(max, min))
	if err != nil {
		panic(err)
	}
	return new(big.Int).Add(num, min)
}

// RandomInt generates a random integer between min and max.
func RandomInt(min, max int) int {
	num, err := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	if err != nil {
		panic(err)
	}
	return int(num.Int64()) + min
}

// TrimHex trims the '0x' prefix from a hex string.
func TrimHex(s string) string {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s[2:]
	}
	return s
}

func PrettyHex(v frontend.Variable) string {
	type hasher interface {
		HashCode() [16]byte
	}
	switch v := v.(type) {
	case (*big.Int):
		return hex.EncodeToString(arbo.BigIntToBytes(32, v)[:4])
	case int:
		return fmt.Sprintf("%d", v)
	case []byte:
		return fmt.Sprintf("%x", v[:4])
	case hasher:
		return fmt.Sprintf("%x", v.HashCode())
	default:
		return fmt.Sprintf("(%v)=%+v", reflect.TypeOf(v), v)
	}
}

// TruncateToLowerBits returns a big.Int truncated to the least-significant `bits`.
func TruncateToLowerBits(input *big.Int, bits uint) *big.Int {
	mask := new(big.Int).Lsh(big.NewInt(1), bits) // 1 << bits
	mask.Sub(mask, big.NewInt(1))                 // (1 << bits) - 1
	return new(big.Int).And(input, mask)          // input & ((1 << bits) - 1)
}
