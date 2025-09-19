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
  const wasmBuffer = fs.readFileSync(path.join(__dirname, 'davinci_crypto.wasm'));
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
    encryptionKey: [
      "9893338637931860616720507408105297162588837225464624604186540472082423274495",
      "12595438123836047903232785676476920953357035744165788772034206819455277990072"
    ],
    k: "964256131946492867709099996647243890828558919187",
    ballotMode: {
      numFields: 5,
      uniqueValues: false,
      maxValue: "16",
      minValue: "0",
      maxValueSum: "1280",
      minValueSum: "5",
      costExponent: 2,
      costFromWeight: false
    },
    weight: "10",
    fieldValues: ["14", "9", "8", "9", "0", "0", "0", "0"]
  };

  console.log('Input data prepared. Waiting for WASM to initialize...');

  // Wait for the DavinciWasm to be initialized
  await waitForDavinciCryptoWasm();

  console.log('WASM initialized.');
  console.log('\n=== Proof inputs generation example ===\n')

  // Call the proofInputs function
  try {
    const result = global.DavinciCrypto.proofInputs(JSON.stringify(inputs));

    if (result.error) {
      console.error('Error generating ballot proof:', result.error);
    } else {
      const parsedResult = result.data;
      console.log('Ballot Proof Generated:');
      console.log('- Vote ID:', parsedResult.voteId);
      console.log('\nFull Circom Inputs:', JSON.stringify(parsedResult.circomInputs, null, 2));
    }
  } catch (err) {
    console.error('Execution error:', err);
  }

  console.log('\n=== CSP Sign and Verify Example ===\n');

  const censusOrigin = 2; // EdDSA over BLS12-377 curve.
  const privKey = "50df49d9d1175d49808602d12bf945ba3f55d90146882fbc5d54078f204f5005372143904f3fd452767581fd55b4c27aedacdd7b70d14f374b7c9f341c0f9a5300";
  const processId = "00000539f39fd6e51aad88f6f4ce6ab8827279cfffb922660000000000000000";
  const address = "0e9eA11b92F119aEce01990b68d85227a41AA627";
  // Call CSP functions
  try {
    const signResult = global.DavinciCrypto.cspSign(censusOrigin, privKey, processId, address);
    const cspProof = signResult.data;
    console.log('CSP Census Proof:', cspProof);
    const verifyResult = global.DavinciCrypto.cspVerify(JSON.stringify(cspProof));
    console.log('\nCSP Proof verification result:', verifyResult);
  } catch (err) {
    console.error('Execution error:', err)
  }
}

// Helper function to wait for DavinciCrypto wasm to be initialized
async function waitForDavinciCryptoWasm(timeoutMs = 5000) {
  const startTime = Date.now();

  while (!global.DavinciCrypto && Date.now() - startTime < timeoutMs) {
    await new Promise(resolve => setTimeout(resolve, 100));
  }

  if (!global.DavinciCrypto) {
    throw new Error(`DavinciCrypto wasm not initialized after ${timeoutMs}ms`);
  }
}

main().catch(err => {
  console.error('Error:', err);
  process.exit(1);
});