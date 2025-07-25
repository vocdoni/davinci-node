// go run scripts/gen_omega_hex.go > blobs/omega_hex.go
package main

import (
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

const (
	n    = 4096
	logN = 12
)

func main() {
	var root fr.Element
	root.SetString("10238227357739495823651030575849232062558860180284477541189508159991286009131")
	exp := new(big.Int).Lsh(big.NewInt(1), 20) // 2^20
	var gen fr.Element
	gen.Exp(root, exp)

	domain := make([]fr.Element, n)
	domain[0].SetOne()
	for i := 1; i < n; i++ {
		domain[i].Mul(&domain[i-1], &gen)
	}

	fmt.Println("// Code generated; DO NOT EDIT.")
	fmt.Println("package blobs")
	fmt.Println()
	fmt.Println("var omegaHex = [4096]string{")
	for i := 0; i < n; i++ {
		idx := bitReverse(i, logN)
		fmt.Printf("\t\"0x%s\",\n", domain[idx].Text(16))
	}
	fmt.Println("}")
	mod := fr.Modulus()
	nInv := new(big.Int).ModInverse(big.NewInt(n), mod)
	fmt.Printf("\nvar nInvHex = \"0x%s\"\n", nInv.Text(16))
}

func bitReverse(x, bits int) int {
	var r int
	for i := 0; i < bits; i++ {
		if x&(1<<i) != 0 {
			r |= 1 << (bits - 1 - i)
		}
	}
	return r
}
