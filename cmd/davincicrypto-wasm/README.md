# DavinciCrypto WASM Module

A WebAssembly module that provides cryptographic functions for secure ballot operations in the Vocdoni voting system.

## Overview

This WASM module enables client-side cryptographic operations for private voting, including:

- Ballot encryption using ElGamal cryptography
- Generation of zero-knowledge proof inputs
- Preparation of circuit inputs for zero-knowledge proofs
- CSP root calculation
- CSP proofs generation and verification

## Usage

### Building

```bash
make build
```

### Example
Take a look at the example in the [`./nodejstest`](./nodejstest/README.md) folder.

### JavaScript Integration

1. Include the `wasm_exec.js` file in your project (provided by the Go toolchain)
2. Load the `davinci_crypto.wasm` binary
3. Access the exported functions via the `DavinciCrypto` global object


#### Ballot proof inputs generation
```javascript
// Example JavaScript usage
const input = JSON.stringify({
  address: "0x...",              // Voter address (hex)
  processID: "0x...",            // Voting process ID (hex)
  encryptionKey: ["...", "..."], // Public key coordinates
  k: "...",                      // Random factor for encryption
  ballotMode: {                  // Voting constraints
    numFields: 5,
    uniqueValues: 0,
    // other ballot mode parameters...
  },
  weight: "10",                  // Voter weight
  fieldValues: ["14", "9", "8", "9", "0", "0", "0", "0"] // Vote values
});

// Generate proof inputs
const result = DavinciCrypto.proofInputs(input);
if (result.error) {
  console.error("Error:", result.error);
} else {
  const proofInputs = result.data;
  console.log("Circuit inputs:", proofInputs.circuitInputs);
  console.log("Signature hash:", proofInputs.signatureHash);
}
```

##### Output

```json
{
  "circuitInputs": {
    "fields": [...],            // Vote values as strings
    "num_fields": "5",           // Maximum selections allowed
    "unique_values": "0",    // Whether each option can only be selected once
    "max_value": "16",          // Maximum value for each field
    "min_value": "0",           // Minimum value for each field
    "max_value_sum": "1280",   // Maximum total cost allowed
    "min_value_sum": "5",      // Minimum total cost required
    "cost_exponent": "2",            // Cost exponent for quadratic voting
    "cost_from_weight": "0",    // Whether cost is derived from weight
    "address": "...",           // Voter address (in circuit format)
    "weight": "10",             // Voter weight
    "process_id": "...",        // Process ID (in circuit format)
    "vote_id": "...",           // Vote ID
    "pk": [...],                // Public key components
    "k": "...",                 // Random factor used in encryption
    "ciphertexts": [...],       // Encrypted ballot
    "inputs_hash": "0x...",     // Hash of all inputs
  },
  "signatureHash": "0x..."      // Hash for signature verification
}
```

#### CSP root calculation

```javascript
const privKey = "50df49d9d1175d49808602d12bf945ba3f55d90146882fbc5d54078f204f5005372143904f3fd452767581fd55b4c27aedacdd7b70d14f374b7c9f341c0f9a5300";
const processID = "00000539f39fd6e51aad88f6f4ce6ab8827279cfffb922660000000000000000";
const address = "0e9eA11b92F119aEce01990b68d85227a41AA627";

try {
  // Root calculation
  const cspRoot = global.DavinciCrypto.cspCensusRoot(censusOrigin, privKey);
  console.log('CSP Census Root:', cspRoot.data);
} catch (err) {
  console.error('Execution error:', err)
}
```

##### Output
```json
{
  "root": "0x054b8528d8057ea9a6f2b80f6ed2eccf16c5f7db7dcc5565ceca166d66a94312"
}
```


#### CSP proof generation and verification

```javascript
const privKey = "50df49d9d1175d49808602d12bf945ba3f55d90146882fbc5d54078f204f5005372143904f3fd452767581fd55b4c27aedacdd7b70d14f374b7c9f341c0f9a5300";
const processID = "00000539f39fd6e51aad88f6f4ce6ab8827279cfffb922660000000000000000";
const address = "0e9eA11b92F119aEce01990b68d85227a41AA627";

try {
  // Proof generation
  const signResult = DavinciCrypto.cspSign(privKey, processID, address);
  const cspProof = signResult.data;
  console.log('CSP Proof:', cspProof);

  // Proof verification
  const verifyResult = DavinciCrypto.cspVerify(strCSPProof);
  console.log('CSP Verify Result:', verifyResult);
} catch (err) {
  console.error('Execution error:', err)
}
```

##### Output

```json
{
  "censusOrigin": 2,
  "root": "0x0b2bb59e8a7a9adc5c458c1da6cff2f41fbacda8fa8d82c0ee2c5217bdd8c6ce",
  "address": "0x0e9ea11b92f119aece01990b68d85227a41aa627",
  "processId": "0x00000539f39fd6e51aad88f6f4ce6ab8827279cfffb922660000000000000000",
  "publicKey": "0x193aaba7106ad4691b9be682c2c5a8ccb6af22ebd4e00cb158b3cd0ed18c0c8f",
  "signature": "0x5375ec2ff428468933171394a7f96984b9970a07520c084f452e0d7443e1c80601f805d09ea1a8d94cb59ab239d2308be0156011b9f2635123f288f600fa0f76"
}
```

#### CSP Census Root Generation

```javascript
const censusOrigin = 2;
const privKeySeed = "50df49d9d1175d49808602d12bf945ba3f55d90146882fbc5d54078f204f5005372143904f3fd452767581fd55b4c27aedacdd7b70d14f374b7c9f341c0f9a5300";

try {
  // Generate census root
  const censusRootResult = DavinciCrypto.cspCensusRoot(censusOrigin, privKeySeed);
  console.log('Census Root Hexadecimal:', censusRootResult);
} catch (err) {
  console.error('Execution error:', err)
}
```