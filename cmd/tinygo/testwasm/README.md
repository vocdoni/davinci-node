# Go to JavaScript via WebAssembly

This example demonstrates how to expose Go functions to JavaScript/Node.js via WebAssembly.

## Overview

This project compiles Go code to WebAssembly (WASM) and makes Go functions callable from JavaScript. 
It uses the standard Go compiler (not TinyGo) and the `syscall/js` package to create JavaScript bindings.

## Files

- `encrypt.wasm` — WebAssembly module compiled from Go source files
- `wasm_exec.js` — Go's official JavaScript support file for WebAssembly
- `index.js` — Node.js example that:
  1. Imports Go's `wasm_exec.js`
  2. Loads and instantiates the WebAssembly module
  3. Calls the exposed Go functions
- `package.json` — npm configuration with build and run scripts

## How It Works

1. **Building the WebAssembly module**: Go code is compiled to WebAssembly using the `GOOS=js GOARCH=wasm` build flags.

2. **JavaScript Bindings**: In `main_wasm.go`, Go functions are exposed to JavaScript using the `syscall/js` package:
   ```go
   js.Global().Set("functionName", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
     // Call Go function
     return result
   }))
   ```

3. **JavaScript Integration**: JavaScript code loads the `wasm_exec.js` support file, instantiates the WebAssembly module with the Go runtime, and calls the exposed functions via the global object.

4. **Data Flow**: 
   - Go → JavaScript: Result values from Go functions are made accessible via the global scope
   - JavaScript → Go: Parameters are passed to Go functions as arguments

## WebAssembly Runtime Architecture

The integration works through these components:

1. **Go Runtime**: The Go code is compiled to WebAssembly, maintaining Go's runtime and garbage collector.

2. **JavaScript Glue**: The `wasm_exec.js` file provides the necessary JavaScript functions to instantiate and run the Go WebAssembly runtime.

3. **Function Registration**: When the Go runtime starts, it registers JavaScript functions in the global scope.

4. **Event Loop Handling**: The Go main function blocks with `select{}` to keep the WebAssembly instance alive, while JavaScript can continue to call the registered functions.

## Available Functions

### Encryption Functions
- `encrypt(value)` - Encrypts a value using BabyJubJub elGamal encryption
- `getResultX1()` - Returns the X coordinate of the first encryption point (C1)
- `getResultY1()` - Returns the Y coordinate of the first encryption point (C1)
- `getResultX2()` - Returns the X coordinate of the second encryption point (C2)
- `getResultY2()` - Returns the Y coordinate of the second encryption point (C2)

### Commitment and Nullifier Functions
- `genCommitmentAndNullifier(address, processID, secret)` - Generates a commitment and nullifier
- `getCommitment()` - Returns the generated commitment
- `getNullifier()` - Returns the generated nullifier

## Prerequisites

- Go 1.20+ (for WebAssembly compilation)
- Node.js 16+ (for running the JavaScript code)

## Usage

```bash
cd testwasm
npm install
npm run start
```

This will:
1. Compile the Go code to WebAssembly
2. Run the Node.js example
3. Execute the cryptographic operations and display the results

## Extending

To add more Go functions:

1. Add a new exported function in the Go code
2. Register it in the `registerCallbacks()` function in `main_wasm.go`
3. Call the function from JavaScript via the global object