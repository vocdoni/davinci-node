import fs from 'fs/promises';
import { WASI } from 'wasi';

async function main() {
  try {
    // Load WASI module
    const wasmBuffer = await fs.readFile(new URL('./encrypt.wasi', import.meta.url));
    
    // Set up WASI environment
    const wasi = new WASI({ 
      version: 'preview1', 
      args: ['node', 'encrypt.wasi'],
      env: process.env, 
      preopens: { '/': '/', './': '.' } 
    });
    
    // Initialize and instantiate the module
    const { instance } = await WebAssembly.instantiate(wasmBuffer, { 
      wasi_snapshot_preview1: wasi.wasiImport 
    });
    
    // Get the functions we need
    const { 
      encrypt, 
      getResultX1, getResultY1,
      getResultX2, getResultY2,
      memory 
    } = instance.exports;
    
    // Start the WASI instance
    wasi.start(instance);
    
    // Test with input value 42
    const inputValue = 42;
    console.log(`Testing encrypt(${inputValue}):`);
    
    // Call the encrypt function
    const status = encrypt(inputValue);
    
    if (status === 1) {
      // Helper function to read a C string from memory
      function readCString(pointer) {
        if (!pointer) return null;
        
        const view = new Uint8Array(memory.buffer);
        let end = pointer;
        while (view[end] !== 0) end++;
        
        return new TextDecoder().decode(
          new Uint8Array(memory.buffer, pointer, end - pointer)
        );
      }
      
      // Get results using the getter functions
      const x1 = readCString(getResultX1());
      const y1 = readCString(getResultY1());
      const x2 = readCString(getResultX2());
      const y2 = readCString(getResultY2());
      
      // Display the result
      console.log('Encryption result:');
      console.log('Point 1:');
      console.log('  x:', x1);
      console.log('  y:', y1);
      console.log('Point 2:');
      console.log('  x:', x2);
      console.log('  y:', y2);
    } else {
      console.error('Encryption failed with status:', status);
    }
  } catch (err) {
    console.error('Error:', err);
  }
}

main();
