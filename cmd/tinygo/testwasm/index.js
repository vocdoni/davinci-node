import fs from 'fs/promises';

async function main() {
  try {
    console.log('Loading encrypt.wasm module...');
    // Load the WebAssembly module built with TinyGo
    const wasmBuffer = await fs.readFile(new URL('./encrypt.wasm', import.meta.url));
    
    // Set up imports for TinyGo - provide both WASI and gojs proxies
    const importObject = {
      // Provide WASI imports required by TinyGo
      wasi_snapshot_preview1: new Proxy({}, {
        get: function(target, prop) {
          console.log(`WASI function requested: ${prop}`);
          return () => 0;
        }
      }),
      // Provide gojs imports required by TinyGo string handling
      gojs: new Proxy({}, {
        get: function(target, prop) {
          console.log(`gojs function requested: ${prop}`);
          return () => 0;
        }
      }),
      env: {
        // Additional env imports that might be needed
      }
    };
    
    console.log('Instantiating WASM module...');
    const { instance } = await WebAssembly.instantiate(wasmBuffer, importObject);
    
    // Extract functions and memory from exports
    const { 
      encrypt, 
      getResultX1, 
      getResultY1, 
      getResultX2, 
      getResultY2, 
      memory 
    } = instance.exports;
    
    if (typeof encrypt !== 'function') {
      console.error('encrypt export is not a function');
      process.exit(1);
    }
    
    console.log('WASM module loaded successfully.');
    console.log('Available exports:', Object.keys(instance.exports).join(', '));
    
    const inputValue = 42;
    console.log(`\nTesting encrypt(${inputValue})...`);
    
    try {
      // Call encrypt function - returns status code (1 for success)
      const status = encrypt(inputValue);
      console.log('Encryption status:', status);
      
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
        
        console.log('\nEncryption result:');
        console.log('Point 1:');
        console.log('  x:', x1);
        console.log('  y:', y1);
        console.log('Point 2:');
        console.log('  x:', x2);
        console.log('  y:', y2);
      } else {
        console.error('Encryption failed with status:', status);
      }
    } catch (error) {
      console.error('\nError in encrypt function:');
      console.error(error);
      console.log('\nThis error is expected if the required cryptographic dependencies');
      console.log('are not properly included in the WASM build.');
    }
  } catch (err) {
    console.error('Error instantiating WASM module:', err);
    process.exit(1);
  }
}

main();
