package recursion

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	curve "github.com/consensys/gnark-crypto/ecc/bn254"
)

// stringToBigInt converts a string to a big.Int, handling both decimal and hexadecimal representations.
func stringToBigInt(s string) (*big.Int, error) {
	if len(s) >= 2 && s[:2] == "0x" {
		bi, ok := new(big.Int).SetString(s[2:], 16)
		if !ok {
			return nil, fmt.Errorf("failed to parse hex string %s", s)
		}
		return bi, nil
	}
	bi, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("failed to parse decimal string %s", s)
	}
	return bi, nil
}

func addZPadding(b []byte) []byte {
	var z [32]byte
	var r []byte
	r = append(r, z[len(b):]...) // add padding on the left
	r = append(r, b...)
	return r[:32]
}

func stringToBytes(s string) ([]byte, error) {
	if s == "1" {
		s = "0"
	}
	bi, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("error parsing bigint stringToBytes")
	}
	b := bi.Bytes()
	if len(b) != 32 { //nolint:gomnd
		b = addZPadding(b)
	}
	return b, nil
}

//nolint:gomnd
func stringToG1(h []string) (*curve.G1Affine, error) {
	if len(h) <= 2 {
		return nil, fmt.Errorf("not enough data for stringToG1")
	}
	h = h[:2]
	hexa := len(h[0]) > 1 && strings.HasPrefix(h[0], "0x")
	in := ""

	var b []byte
	var err error
	if hexa {
		for i := range h {
			in += strings.TrimPrefix(h[i], "0x")
		}
		b, err = hex.DecodeString(in)
		if err != nil {
			return nil, err
		}
	} else {
		// TODO TMP
		// TODO use stringToBytes()
		if h[0] == "1" {
			h[0] = "0"
		}
		if h[1] == "1" {
			h[1] = "0"
		}
		bi0, ok := new(big.Int).SetString(h[0], 10)
		if !ok {
			return nil, fmt.Errorf("error parsing stringToG1")
		}
		bi1, ok := new(big.Int).SetString(h[1], 10)
		if !ok {
			return nil, fmt.Errorf("error parsing stringToG1")
		}
		b0 := bi0.Bytes()
		b1 := bi1.Bytes()
		if len(b0) != 32 {
			b0 = addZPadding(b0)
		}
		if len(b1) != 32 {
			b1 = addZPadding(b1)
		}

		b = append(b, b0...)
		b = append(b, b1...)
	}
	p := new(curve.G1Affine)
	err = p.Unmarshal(b)

	return p, err
}

func stringToG2(h [][]string) (*curve.G2Affine, error) {
	if len(h) <= 2 { //nolint:gomnd
		return nil, fmt.Errorf("not enough data for stringToG2")
	}
	h = h[:2]
	hexa := len(h[0][0]) > 1 && strings.HasPrefix(h[0][0], "0x")
	in := ""
	var b []byte
	var err error
	if hexa {
		for i := 0; i < len(h); i++ {
			for j := 0; j < len(h[i]); j++ {
				in += strings.TrimPrefix(h[i][j], "0x")
			}
		}
		b, err = hex.DecodeString(in)
		if err != nil {
			return nil, err
		}
	} else {
		// TODO TMP
		bH, err := stringToBytes(h[0][1])
		if err != nil {
			return nil, err
		}
		b = append(b, bH...)
		bH, err = stringToBytes(h[0][0])
		if err != nil {
			return nil, err
		}
		b = append(b, bH...)
		bH, err = stringToBytes(h[1][1])
		if err != nil {
			return nil, err
		}
		b = append(b, bH...)
		bH, err = stringToBytes(h[1][0])
		if err != nil {
			return nil, err
		}
		b = append(b, bH...)
	}

	p := new(curve.G2Affine)
	err = p.Unmarshal(b)
	return p, err
}
