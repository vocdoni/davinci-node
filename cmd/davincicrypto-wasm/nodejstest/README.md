# DavinciCrypto WebAssembly Node.js Demo

This project demonstrates how to use the DavinciCrypto WebAssembly module from a Node.js application.

## Overview

This demo compiles Go code to WebAssembly (WASM) and makes Go functions callable from Node.js. 
It uses the standard Go compiler (not TinyGo) and the `syscall/js` package to create JavaScript bindings.

## Files

- `davinci_crypto.wasm` — WebAssembly module compiled from Go source files
- `wasm_exec.js` — Go's official JavaScript support file for WebAssembly
- `index.js` — Node.js example that:
  1. Imports Go's `wasm_exec.js`
  2. Loads and instantiates the WebAssembly module
  3. Calls the exposed Go functions
- `package.json` — npm configuration with build and run scripts

## How It Works

1. **Building the WebAssembly module**: Go code is compiled to WebAssembly using the `GOOS=js GOARCH=wasm` build flags.

2. **JavaScript Bindings**: In the Go code, functions are exposed to JavaScript using the `syscall/js` package.

3. **JavaScript Integration**: JavaScript code loads the `wasm_exec.js` support file, instantiates the WebAssembly module with the Go runtime, and calls the exposed functions via the global object.

4. **Data Flow**: 
   - Go → JavaScript: Results are returned as JSON strings via the global scope
   - JavaScript → Go: Parameters are passed to Go functions as JSON strings

## Cryptographic Operations

The following cryptographic operations are performed:

1. **Ballot Encryption**: Simulates the encryption of ballot field values (in a real implementation, this would use elliptic curve cryptography with the ElGamal algorithm).

2. **CSP proof generation and verification**: Generates and verifies a CSP proof for an address and a process ID.

## Available Functions

The following functions are available on the `BallotProofWasm` global object:
- `proofInputs(jsonInputs)` - Generates ballot proof circuit inputs from a JSON string input.
- `cspSign(censusOrigin, privKey, processId, address)` - Generates a CSP proof for the process ID and voter address.
- `cspVerify(jsonProof)` - Verifies the proof provided.

## Prerequisites

- Go 1.20+ (for WebAssembly compilation)
- Node.js 16+ (for running the JavaScript code)

## Usage

### Using Make (Recommended)

```bash
cd nodejstest
make test     # Build and run
```

### Using npm scripts

```bash
cd nodejstest
npm run test   # Build and run the example
```

Note: The `wasm_exec.js` file from tinygo/testwasm is already included in this directory.

This will:
1. Compile the Go code to WebAssembly
2. Run the Node.js example
3. Generate a ballot proof and display the results
