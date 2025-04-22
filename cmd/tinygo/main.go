package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	bjj "davinci-wasm/bjj"
)

const (
	pubKeyX = "17107410299102466023288795078467125527215278567930593653919750689119095810843"
	pubKeyY = "8909387280870685879207330388060711921507538330788436921948790826652954104985"
)

// Global variables to store result strings - will be accessed through getters
var (
	resultX1   = ""
	resultY1   = ""
	resultX2   = ""
	resultY2   = ""
	commitment = ""
	nullifier  = ""
)

//export genCommitmentAndNullifier
func genCommitmentAndNullifier(address, processID, secret string) int32 {
	// Convert strings to byte slices
	addressBytes, err := hex.DecodeString(trimHex(address))
	if err != nil {
		panic("failed to decode address")
	}
	processIDBytes, err := hex.DecodeString(trimHex(processID))
	if err != nil {
		panic("failed to decode processID")
	}
	secretBytes, err := hex.DecodeString(trimHex(secret))
	if err != nil {
		panic("failed to decode secret")
	}
	commitmentBI, nullifierBI, err := GenCommitmentAndNullifier(addressBytes, processIDBytes, secretBytes)
	if err != nil {
		panic("failed to generate commitment and nullifier")
	}
	commitment = commitmentBI.String()
	nullifier = nullifierBI.String()
	return 1 // Return success code
}

//export encrypt
func encrypt(val int32) int32 {
	pubKey := bjj.New()
	x, ok := new(big.Int).SetString(pubKeyX, 10)
	if !ok {
		panic("failed to set pubKeyX")
	}
	y, ok := new(big.Int).SetString(pubKeyY, 10)
	if !ok {
		panic("failed to set pubKeyY")
	}
	pubKey = pubKey.SetPoint(x, y)
	msg := new(big.Int).SetInt64(int64(val))
	k := new(big.Int).SetInt64(1)
	c1, c2, err := EncryptWithK(pubKey, msg, k)
	if err != nil {
		panic("failed to encrypt")
	}
	x1, y1 := c1.Point()
	x2, y2 := c2.Point()

	// Store results in global variables
	resultX1 = x1.String()
	resultY1 = y1.String()
	resultX2 = x2.String()
	resultY2 = y2.String()

	return 1 // Return success code
}

//export getResultX1
func getResultX1() *byte {
	return stringToPtr(resultX1)
}

//export getResultY1
func getResultY1() *byte {
	return stringToPtr(resultY1)
}

//export getResultX2
func getResultX2() *byte {
	return stringToPtr(resultX2)
}

//export getResultY2
func getResultY2() *byte {
	return stringToPtr(resultY2)
}

//export getCommitment
func getCommitment() *byte {
	return stringToPtr(commitment)
}

//export getNullifier
func getNullifier() *byte {
	return stringToPtr(nullifier)
}

// Helper function to convert a Go string to a pointer to a C string
func stringToPtr(s string) *byte {
	// Append null terminator for C string
	if len(s) == 0 {
		return &[]byte{0}[0]
	}
	bytes := []byte(s)
	bytes = append(bytes, 0) // null terminator
	return &bytes[0]
}

// The main entrypoint for WASI; kept empty to prevent deadlocks.
func main() {
	fmt.Println("WASI module initialized") // This won't be seen, but makes sure fmt is linked
}

// EncryptWithK function encrypts a message using the public key provided as
// elliptic curve point and the random k value provided. It returns the two
// points that represent the encrypted message and error if any.
//
// TODO: remove error return, since it can never error
func EncryptWithK(pubKey *bjj.BJJ, msg, k *big.Int) (*bjj.BJJ, *bjj.BJJ, error) {
	// Check for nil inputs to avoid panics
	if pubKey == nil {
		return nil, nil, errors.New("pubKey is nil")
	}
	if msg == nil {
		return nil, nil, errors.New("msg is nil")
	}
	if k == nil {
		return nil, nil, errors.New("k is nil")
	}

	order := pubKey.Order()
	// ensure the message is within the field
	msgCopy := new(big.Int).Mod(msg, order) // Create a new big.Int to avoid modifying the input

	// compute C1 = k * G
	c1 := pubKey.New()
	if c1 == nil {
		return nil, nil, errors.New("failed to create c1 point")
	}
	c1.ScalarBaseMult(k)

	// compute s = k * pubKey
	s := pubKey.New()
	if s == nil {
		return nil, nil, errors.New("failed to create s point")
	}
	s.ScalarMult(pubKey, k)

	// encode message as point M = message * G
	m := pubKey.New()
	if m == nil {
		return nil, nil, errors.New("failed to create m point")
	}
	m.ScalarBaseMult(msgCopy)

	// compute C2 = M + s
	c2 := pubKey.New()
	if c2 == nil {
		return nil, nil, errors.New("failed to create c2 point")
	}
	c2.Add(m, s)

	return c1, c2, nil
}
