import fs from 'fs/promises';

async function main() {
  console.log('Testing encrypt.wasm functionality...');
  
  // Load the WebAssembly module built with TinyGo
  const wasmBuffer = await fs.readFile(new URL('./encrypt.wasm', import.meta.url));
  
  // Create a stub WASI implementation
  const wasi = {
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
    env: {}
  };
  
  // Instantiate with import object for TinyGo
  const { instance } = await WebAssembly.instantiate(wasmBuffer, importObject);
  
  // Extract the encrypt function from exports
  const { encrypt } = instance.exports;
  if (typeof encrypt !== 'function') {
    console.error('encrypt export is not a function');
    process.exit(1);
  }
  
  // Test various input values
  const testCases = [
    { input: 0, expected: 0 },
    { input: 1, expected: 2 },
    { input: -5, expected: -10 },
    { input: 42, expected: 84 },
    { input: 1000, expected: 2000 }
  ];
  
  console.log('Running test cases:');
  let allPassed = true;
  
  for (const testCase of testCases) {
    const result = encrypt(testCase.input);
    const passed = result === testCase.expected;
    
    console.log(`encrypt(${testCase.input}) = ${result} [${passed ? 'PASS' : 'FAIL'}]`);
    
    if (!passed) {
      allPassed = false;
      console.error(`  Expected: ${testCase.expected}, Got: ${result}`);
    }
  }
  
  console.log(`\nTest summary: ${allPassed ? 'ALL TESTS PASSED' : 'SOME TESTS FAILED'}`);
  return allPassed ? 0 : 1;
}

main().then(exitCode => {
  process.exit(exitCode);
}).catch(err => {
  console.error('Error executing wasm:', err);
  process.exit(1);
});
