import fs from 'fs/promises';

async function main() {
  console.log('Testing encrypt.wasm functionality...');
  
  try {
    // Load the WebAssembly module built with TinyGo
    const wasmBuffer = await fs.readFile(new URL('./encrypt.wasm', import.meta.url));
    
    // Pure WebAssembly approach using Proxy to handle any WASI imports
    const { instance } = await WebAssembly.instantiate(wasmBuffer, {
      // Create a proxy that returns dummy functions for any WASI imports
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
  } catch (err) {
    console.error('Error executing wasm:', err);
    process.exit(1);
  }
}

main().then(exitCode => {
  process.exit(exitCode);
});
