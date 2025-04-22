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
      }),
      // Add gojs module required by TinyGo for string handling
      gojs: new Proxy({}, {
        get: function(target, prop) {
          return () => 0;
        }
      }),
      env: {
        // Any additional env imports that TinyGo might need
      }
    });
    
    // Extract the encrypt function and memory from exports
    const { encrypt, memory } = instance.exports;
    if (typeof encrypt !== 'function') {
      console.error('encrypt export is not a function');
      process.exit(1);
    }
    
    // Test various input values
    const testInputs = [0, 1, 42, 100, 1000];
    
    console.log('Running test cases:');
    let allPassed = true;
    
    for (const input of testInputs) {
      console.log(`\nTesting encrypt(${input}):`);
      
      // Call encrypt function which returns a pointer to the string array
      const result = encrypt(input);
      
      // Check if we got a valid result pointer
      if (result === 0) {
        console.error(`  Failed: encrypt(${input}) returned null pointer`);
        allPassed = false;
        continue;
      }
      
      // Parse the string array from memory
      try {
        // Access the array data
        const view = new DataView(memory.buffer);
        
        // Get array length
        const len = view.getInt32(result, true); // Little endian
        console.log(`  Array length: ${len}`);
        
        if (len !== 4) {
          console.error(`  Failed: Expected 4 strings, got ${len}`);
          allPassed = false;
          continue;
        }
        
        // Calculate the offset to the array data
        const arrayPtr = result + 4; // Skip the length field
        
        // Extract each string from the array
        const strings = [];
        for (let i = 0; i < len; i++) {
          // Each element is a struct with pointer and length (8 bytes each)
          const elemPtr = arrayPtr + (i * 8);
          const strPtr = view.getInt32(elemPtr, true);
          const strLen = view.getInt32(elemPtr + 4, true);
          
          if (strPtr === 0) {
            throw new Error(`String pointer is null for element ${i}`);
          }
          
          // Read the string data
          const bytes = new Uint8Array(memory.buffer, strPtr, strLen);
          const str = new TextDecoder().decode(bytes);
          strings.push(str);
        }
        
        // Validate that we got 4 strings representing two points
        console.log('  Point 1:');
        console.log(`    x: ${strings[0]}`);
        console.log(`    y: ${strings[1]}`);
        console.log('  Point 2:');
        console.log(`    x: ${strings[2]}`);
        console.log(`    y: ${strings[3]}`);
        
        // Validate strings are numeric (valid big integers)
        const validStrings = strings.every(str => {
          try {
            // Attempt to parse as BigInt to ensure it's a valid numeric string
            BigInt(str);
            return true;
          } catch (e) {
            console.error(`  Failed: "${str}" is not a valid numeric string`);
            return false;
          }
        });
        
        if (!validStrings) {
          allPassed = false;
        } else {
          console.log(`  PASS: encrypt(${input}) returned valid elliptic curve points`);
        }
      } catch (err) {
        console.error(`  Failed to parse result for encrypt(${input}):`, err);
        allPassed = false;
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
