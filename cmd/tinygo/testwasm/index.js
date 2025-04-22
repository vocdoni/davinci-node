// Entry point for Go WebAssembly with Node.js
console.log('Starting Go WebAssembly example...');
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { dirname } from 'path';

// Import Go's WebAssembly support file
import './wasm_exec.js';

// Get __dirname equivalent in ESM
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// The Go constructor is now available from the global scope
const Go = globalThis.Go;

async function main() {
  // Create a new Go instance
  const go = new Go();

  // Load the WebAssembly module
  console.log('Loading WebAssembly module...');
  const wasmBuffer = fs.readFileSync(path.join(__dirname, 'encrypt.wasm'));
  const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);

  // Start the Go runtime (this will register the JavaScript callbacks)
  console.log('Starting Go runtime...');
  // The Go instance will execute until it blocks on select{} in main()
  go.run(instance).catch(() => {/* ignore wasmExit */});

  // Demo 1: elGamal Encryption
  console.log('\n=== elGamal Encryption Example ===');
  const value = 42;
  console.log(`Encrypting value: ${value}`);
  
  // Call the encrypt function exposed by the Go code
  global.encrypt(value);
  
  // Retrieve the encryption results
  const encryptionResults = {
    c1: { x: global.getResultX1(), y: global.getResultY1() },
    c2: { x: global.getResultX2(), y: global.getResultY2() }
  };
  
  console.log('Encrypted result:');
  console.log('C1:', encryptionResults.c1);
  console.log('C2:', encryptionResults.c2);
  
  // Demo 2: Commitment and Nullifier Generation
  console.log('\n=== Commitment and Nullifier Example ===');
  const address = '0x1234567890abcdef1234567890abcdef12345678';
  const processID = '0xabcdef1234567890abcdef1234567890abcdef12';
  const secret = '0x9876543210abcdef9876543210abcdef98765432';
  
  console.log('Inputs:');
  console.log('- Address:', address);
  console.log('- ProcessID:', processID);
  console.log('- Secret:', secret);
  
  // Generate the commitment and nullifier
  global.genCommitmentAndNullifier(address, processID, secret);
  
  // Retrieve the results
  const commitment = global.getCommitment();
  const nullifier = global.getNullifier();
  
  console.log('Results:');
  console.log('- Commitment:', commitment);
  console.log('- Nullifier:', nullifier);
}

main().catch(err => {
  console.error('Error:', err);
  process.exit(1);
});