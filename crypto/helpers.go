package crypto

import "math/big"

const SerializedFieldSize = 32 // bytes

// BigIntToFFwithPadding transform the inputs bigInt to the field provided, if it is
// not done, the circuit will transform it during the witness calculation and
// the resulting hash will be different. Moreover, the input hash should be
// 32 bytes so if it is not, fill with zeros at the beginning of the bytes
// representation.
func BigIntToFFwithPadding(input, base *big.Int) []byte {
	hash := BigToFF(base, input).Bytes()
	for len(hash) < SerializedFieldSize {
		hash = append([]byte{0}, hash...)
	}
	return hash
}

// BigToFF function returns the finite field representation of the big.Int
// provided. It uses the curve scalar field to represent the provided number.
func BigToFF(baseField, iv *big.Int) *big.Int {
	z := big.NewInt(0)
	if c := iv.Cmp(baseField); c == 0 {
		return z
	} else if c != 1 && iv.Cmp(z) != -1 {
		return iv
	}
	return z.Mod(iv, baseField)
}
