import fs from 'fs/promises';

async function main() {
  // Load the WebAssembly module built with TinyGo
  const wasmBuffer = await fs.readFile(new URL('./encrypt.wasm', import.meta.url));
  
  // Create a stub WASI implementation
  const wasi = {
    // Just provide empty implementations of required functions
    fd_write: () => 0,
    fd_close: () => 0,
    fd_seek: () => 0,
    fd_read: () => 0,
    proc_exit: () => 0,
    environ_sizes_get: () => 0,
    environ_get: () => 0,
    args_sizes_get: () => 0,
    args_get: () => 0,
    random_get: () => 0
  };
  
  // TinyGo imports object with WASI stubs
  const importObject = {
    wasi_snapshot_preview1: wasi,
    env: {
      // You can provide additional env imports if needed
    }
  };
  
  // Instantiate with import object for TinyGo
  const { instance } = await WebAssembly.instantiate(wasmBuffer, importObject);
  
  // Extract the encrypt function from exports
  const { encrypt } = instance.exports;
  if (typeof encrypt !== 'function') {
    console.error('encrypt export is not a function');
    process.exit(1);
  }
  
  const inputValue = 42;
  const result = encrypt(inputValue);
  console.log('encrypt(', inputValue, ') =', result);
}

main().catch(err => {
  console.error('Error executing wasm:', err);
  process.exit(1);
});
