# Ballotproof WASM Module

A WebAssembly module that provides cryptographic functions for secure ballot operations in the Vocdoni voting system.

## Overview

This WASM module enables client-side cryptographic operations for private voting, including:

- Ballot encryption using ElGamal cryptography
- Generation of zero-knowledge proof inputs
- Preparation of circuit inputs for zero-knowledge proofs

## Usage

### Building

```bash
make build
```

### JavaScript Integration

1. Include the `wasm_exec.js` file in your project (provided by the Go toolchain)
2. Load the `ballotproof.wasm` binary
3. Access the exported functions via the `BallotProofWasm` global object

```javascript
// Example JavaScript usage
const input = JSON.stringify({
  address: "0x...",              // Voter address (hex)
  processID: "0x...",            // Voting process ID (hex)
  secret: "0x...",               // Voter secret (hex)
  encryptionKey: ["...", "..."], // Public key coordinates
  k: "...",                      // Random factor for encryption
  ballotMode: {                  // Voting constraints
    maxCount: 5,
    forceUniqueness: 0,
    // other ballot mode parameters...
  },
  weight: "10",                  // Voter weight
  fieldValues: ["14", "9", "8", "9", "0", "0", "0", "0"] // Vote values
});

// Generate proof inputs
const result = BallotProofWasm.proofInputs(input);
if (result.error) {
  console.error("Error:", result.error);
} else {
  const proofInputs = JSON.parse(result.data);
  console.log("Circuit inputs:", proofInputs.circuitInputs);
  console.log("Signature hash:", proofInputs.signatureHash);
}
```

## Key Components

- **Ballot Encryption**: Uses ElGamal encryption to protect vote privacy
- **Circuit Inputs**: Prepares the data needed for zero-knowledge proof generation

## Output Format

The module returns a JSON object with the following structure:

```json
{
  "circuitInputs": {
    "fields": [...],           // Vote values as strings
    "maxCount": "5",           // Maximum selections allowed
    "forceUniqueness": "0",    // Whether each option can only be selected once
    "maxValue": "16",          // Maximum value for each field
    "minValue": "0",           // Minimum value for each field
    "maxTotalCost": "1280",    // Maximum total cost allowed
    "minTotalCost": "5",       // Minimum total cost required
    "costExp": "2",            // Cost exponent for quadratic voting
    "costFromWeight": "0",     // Whether cost is derived from weight
    "address": "...",          // Voter address (in circuit format)
    "weight": "10",            // Voter weight
    "processId": "...",        // Process ID (in circuit format)
    "pk": [...],               // Public key components
    "k": "...",                // Random factor used in encryption
    "ballot": {                // Encrypted ballot
      "curveType": "bjj_gnark",
      "ciphertexts": [...]
    },
    "inputsHash": "0x...",     // Hash of all inputs
    "inputsHashBigInt": "..."  // Big integer representation of input hash
  },
  "signatureHash": "0x..."     // Hash for signature verification
}
```

## Example Output

```json
{
  "fields": ["14", "9", "8", "9", "0", "0", "0", "0"],
  "maxCount": "5",
  "forceUniqueness": "0",
  "maxValue": "16",
  "minValue": "0",
  "maxTotalCost": "1280",
  "minTotalCost": "5",
  "costExp": "2",
  "costFromWeight": "0",
  "address": "328210058572563315673101812097180311317472617309",
  "weight": "10",
  "processId": "177648853222127381170342196429803609279542862380",
  "pk": [
    "15330365222328475080351748221248422512031748173899679740758072665350082079298",
    "12595438123836047903232785676476920953357035744165788772034206819455277990072"
  ],
  "k": "964256131946492867709099996647243890828558919187",
  "ballot": {
    "curveType": "bjj_gnark",
    "ciphertexts": [
      {
        "c1": [
          "9449164171950070230841173983300827345748793243954811907123552759962822350801",
          "1433996667810644508479374607996177420060814504788240126901844770653067489314"
        ],
        "c2": [
          "8478938071917774379616262600217256580090204437452206376393077033224799492550",
          "3073444055146026108025353788454476563533100746301053329624616860325500392325"
        ]
      }
      // Additional ciphertexts...
    ]
  },
  "inputsHash": "0x075b7831e5e4900990e5735ed498b75f40245c2e174b3dc7c5f9c00aff95a9b5",
  "inputsHashBigInt": "20216726076643980110130881325941807260575759913044419930205112959360291940791"
}
