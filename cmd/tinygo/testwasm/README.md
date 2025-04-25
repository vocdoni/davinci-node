<!-- README for testwasm directory -->
# testwasm

This directory demonstrates how to run a TinyGo-compiled WebAssembly module (`encrypt.wasm`) from Node.js
without relying on the WASI interface or hand-rolled import stubs. Instead, it uses TinyGo’s own runtime loader
(`wasm_exec.js`) to provide all required imports and bridge Go’s runtime into JavaScript.

## Files
- `encrypt.wasm` — The WebAssembly binary produced by TinyGo.
- `wasm_exec.js` — TinyGo’s WebAssembly runtime loader, patched for ES module support.
- `index.js` — Node.js script that:
  1. Imports the `Go` loader from `wasm_exec.js`.
  2. Instantiates the WASM module with `go.importObject`.
  3. Runs the Go runtime with `go.run(instance)`.
  4. Calls the exported `encrypt` function, reads its string results, and prints them.
- `package.json` — npm scripts to build and start the demo.

## Setup & Run
```bash
cd testwasm
npm install
npm run start
```

## How It Works

1. **WebAssembly Compilation**
   - We compile the Go code via TinyGo:
     ```bash
     tinygo build \
       -o encrypt.wasm \
       -target wasm \
       -scheduler asyncify \
       -no-debug ..
     ```
   - Using `-scheduler=asyncify` ensures Go’s lightweight scheduler and the `syscall/js`
     bridge work correctly in a single-threaded JavaScript environment.

2. **Runtime Loader (`wasm_exec.js`)**
   - TinyGo provides a `wasm_exec.js` loader that implements:
     - **WASI syscalls** (e.g. `fd_write`, `proc_exit`, `random_get`) to handle I/O and exit.
     - **`syscall/js` bridging** (e.g. `valueGet`, `valueSet`, BigInt conversions) to map between
       Go values and JavaScript values.
     - **Go scheduler** and **Asyncify hooks** to support gopher goroutines and async sleeping.
   - We copied this loader into the `testwasm` folder and appended:
     ```js
     // After the IIFE:
     const Go = global.Go;
     export { Go };
     ```
   - This allows us to use it as an ES module:
     ```js
     import { Go } from './wasm_exec.js';
     ```

3. **Node.js Script (`index.js`)**
   - Loads the WASM binary as a `Buffer`.
   - Creates a `Go` instance:
     ```js
     const go = new Go();
     ```
   - Instantiates the module with `go.importObject`:
     ```js
     const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);
     ```
   - Runs the Go runtime:
     ```js
     await go.run(instance);
     ```
   - Calls `encrypt(val)` and reads back four null-terminated strings from memory.

## Why This Was Necessary

TinyGo’s WebAssembly output depends on a full Go runtime environment:
- Low-level WASI syscalls for I/O and random numbers.
- JavaScript bridging for `syscall/js`; particularly BigInt (i64) support.
- A scheduler or Asyncify mechanism to manage goroutine lifecycles.

Hand-rolling Proxy stubs for these imports was brittle and incomplete: BigInt conversions
failed, and certain syscalls (`proc_exit`, `random_get`) were unimplemented or mis-typed,
leading to runtime panics.

By using TinyGo’s own `wasm_exec.js`, we satisfy all runtime requirements out of the box,
ensuring correct initialization and seamless interop between Go and Node.js.
