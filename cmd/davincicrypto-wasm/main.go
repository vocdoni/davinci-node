//go:build js && wasm
// +build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/circuits/ballotproof"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/types"
)

const (
	jsClassName            = "DavinciCrypto"
	jsBallotProofInputs    = "proofInputs"
	jsCSPSign              = "cspSign"
	jsCSPVerify            = "cspVerify"
	ballotProofInputsNArgs = 1 // ballotproof.BallotProofInputs json encoded string
	cspSignNArgs           = 4 // census origin as uint8 and hex strings with privKey seed, processID and address
	cspVerifyNArgs         = 1 // types.CensusProof json encoded string
)

func generateProofInputs(args []js.Value) any {
	if len(args) != ballotProofInputsNArgs {
		return JSResult(nil, fmt.Errorf("Invalid number of arguments, expected %d got %d", ballotProofInputsNArgs, len(args)))
	}
	// parse the inputs from the first argument
	inputs, err := FromJSONValue[ballotproof.BallotProofInputs](args[0])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid inputs: %v", err))
	}
	// generate the circom inputs
	circomInputs, err := ballotproof.GenerateBallotProofInputs(&inputs)
	if err != nil {
		return JSResult(nil, fmt.Errorf("Error generating circom inputs: %v", err))
	}
	// encode result to json to return it
	bRes, err := json.Marshal(circomInputs)
	if err != nil {
		return JSResult(nil, fmt.Errorf("Error marshaling result: %v", err.Error()))
	}
	return JSResult(string(bRes))
}

func cspSign(args []js.Value) any {
	if len(args) != cspSignNArgs {
		return JSResult(nil, fmt.Errorf("Invalid number of arguments, expected %d got %d", cspSignNArgs, len(args)))
	}
	// decode the census origin from the first argument
	uCensusOrigin, err := FromUint8(args[0])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid census origin decoding: %v", err))
	}
	censusOrigin := types.CensusOrigin(uCensusOrigin)
	if !censusOrigin.Valid() {
		return JSResult(nil, fmt.Errorf("Invalid census origin: %v", err))
	}
	// decode the private key seed from the second argument
	privKeySeed, err := FromHexBytes(args[1])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid private key seed: %v", err))
	}
	// decode the process ID from the third argument
	bProcessID, err := FromHexBytes(args[2])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid process ID decoding: %v", err))
	}
	processID := new(types.ProcessID).SetBytes(bProcessID)
	if !processID.IsValid() {
		return JSResult(nil, fmt.Errorf("Invalid process ID: %s", bProcessID.String()))
	}
	// decode the address from the fourth argument
	bAddress, err := FromHexBytes(args[3])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid address: %v", err))
	}
	address := common.Address(bAddress)
	// initialize the credential service providers with the private key seed
	// decoded
	csp, err := csp.New(censusOrigin, privKeySeed)
	if err != nil {
		return JSResult(nil, fmt.Errorf("CSP cannot be initialized with the provided seed: %w", err))
	}
	// generate the census proof with the process ID and address
	cspProof, err := csp.GenerateProof(processID, address)
	if err != nil {
		return JSResult(nil, fmt.Errorf("Error generating census proof: %v", err))
	}
	// encode the census proof to json to return it
	bRes, err := json.Marshal(cspProof)
	if err != nil {
		return JSResult(nil, fmt.Errorf("Error marshaling result: %v", err.Error()))
	}
	return JSResult(string(bRes))
}

func cspVerify(args []js.Value) any {
	if len(args) != cspVerifyNArgs {
		return JSResult(nil, fmt.Errorf("Invalid number of arguments, expected %d got %d", cspVerifyNArgs, len(args)))
	}
	// decode the census proof from the first argument
	cspProof, err := FromJSONValue[types.CensusProof](args[0])
	if err != nil {
		return JSResult(nil, fmt.Errorf("Invalid census proof: %v", err))
	}
	// verify the census proof with the process ID and address
	if err := csp.VerifyCensusProof(&cspProof); err != nil {
		return JSResult(nil, fmt.Errorf("Census proof verification failed: %v", err))
	}
	// if verification is successful, return true
	return JSResult(true)
}

// main sets up the JavaScript interface and starts the WASM module
func main() {
	// Create an object to hold the BallotProofWasm functions
	ballotProofClass := js.ValueOf(map[string]any{})
	// Register the proofInputs function
	ballotProofClass.Set(jsBallotProofInputs, js.FuncOf(func(this js.Value, args []js.Value) any {
		return generateProofInputs(args)
	}))
	// Register the cspSign function
	ballotProofClass.Set(jsCSPSign, js.FuncOf(func(this js.Value, args []js.Value) any {
		return cspSign(args)
	}))
	// Register the cspVerify function
	ballotProofClass.Set(jsCSPVerify, js.FuncOf(func(this js.Value, args []js.Value) any {
		return cspVerify(args)
	}))
	// Register the class in the global scope so it can be accessed from JavaScript
	js.Global().Set(jsClassName, ballotProofClass)
	// Keep the Go program running
	select {}
}
