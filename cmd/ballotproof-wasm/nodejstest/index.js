// Entry point for Go WebAssembly with Node.js
console.log('Starting BallotProof WebAssembly example...');
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
  const wasmBuffer = fs.readFileSync(path.join(__dirname, 'ballotproof.wasm'));
  const { instance } = await WebAssembly.instantiate(wasmBuffer, go.importObject);

  // Start the Go runtime (this will register the JavaScript callbacks)
  console.log('Starting Go runtime...');
  // The Go instance will execute until it blocks on select{} in main()
  go.run(instance).catch((err) => {
    console.error('Go runtime error:', err);
  });

  // Test BallotProof generation
  console.log('\n=== BallotProof Generation Example ===');
  
  // Prepare the input data
  const inputs = {
    address: "397d72b25676d42f899b18f06633fab9d854235d",
    processID: "1f1e0cd27b4ecd1b71b6333790864ace2870222c",
    secret: "881f648d417540772883ea70e3592d36",
    encryptionKey: [
      "9893338637931860616720507408105297162588837225464624604186540472082423274495",
      "12595438123836047903232785676476920953357035744165788772034206819455277990072"
    ],
    k: "964256131946492867709099996647243890828558919187",
    ballotMode: {
      maxCount: 5,
      forceUniqueness: false,
      maxValue: "16",
      minValue: "0", 
      maxTotalCost: "1280",
      minTotalCost: "5",
      costExponent: 2,
      costFromWeight: false
    },
    weight: "10",
    fieldValues: ["14", "9", "8", "9", "0", "0", "0", "0"]
  };
  
  console.log('Input data prepared. Waiting for WASM to initialize...');
  
  // Wait for the BallotProofWasm to be initialized
  await waitForBallotProofWasm();
  
  console.log('WASM initialized, calling proofInputs function...');
  
  // Call the proofInputs function
  try {
    const result = global.BallotProofWasm.proofInputs(JSON.stringify(inputs));
    
    if (result.error) {
      console.error('Error generating ballot proof:', result.error);
    } else {
      const parsedResult = JSON.parse(result.data);
      console.log('\nBallot Proof Generated:');
      console.log('- Commitment:', parsedResult.circuitInputs.commitment);
      console.log('- Nullifier:', parsedResult.circuitInputs.nullifier);
      console.log('- Signature Hash:', parsedResult.signatureHash);
      console.log('\nFull Circuit Inputs:', JSON.stringify(parsedResult.circuitInputs, null, 2));
    }
  } catch (err) {
    console.error('Execution error:', err);
  }
}

// Helper function to wait for BallotProofWasm to be initialized
async function waitForBallotProofWasm(timeoutMs = 5000) {
  const startTime = Date.now();
  
  while (!global.BallotProofWasm && Date.now() - startTime < timeoutMs) {
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  
  if (!global.BallotProofWasm) {
    throw new Error(`BallotProofWasm not initialized after ${timeoutMs}ms`);
  }
}

main().catch(err => {
  console.error('Error:', err);
  process.exit(1);
});