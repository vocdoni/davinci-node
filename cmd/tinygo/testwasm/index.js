import fs from 'fs/promises';

async function main() {
  try {
    // Load the WebAssembly module built with TinyGo
    const wasmBuffer = await fs.readFile(new URL('./encrypt.wasm', import.meta.url));
    
    // Pure WebAssembly approach - provide a custom instantiation function
    // that ignores any WASI imports TinyGo might include
    const { instance } = await WebAssembly.instantiate(wasmBuffer, {
      // This is a cleaner approach - we create an importObject that satisfies
      // any imports the module needs, but we do it by creating a proxy that
      // returns dummy functions for any undefined imports
      // This avoids having to list all WASI function stubs
      wasi_snapshot_preview1: new Proxy({}, {
        get: function(target, prop) {
          // Return a no-op function for any WASI import
          return () => 0;
        }
      })
    });
    
    // Extract the encrypt function from exports
    const { encrypt } = instance.exports;
    if (typeof encrypt !== 'function') {
      console.error('encrypt export is not a function');
      process.exit(1);
    }
    
    const inputValue = 42;
    const result = encrypt(inputValue);
    console.log('encrypt(', inputValue, ') =', result);
  } catch (err) {
    console.error('Error:', err);
    process.exit(1);
  }
}

main();
