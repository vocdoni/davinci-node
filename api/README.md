# API Documentation

This document describes the HTTP API endpoints for the Vocdoni Z Sandbox API server.

## Table of Contents

- [Base URL](#base-url)
- [Response Format](#response-format)
- [Error Handling](#error-handling)
- [Endpoints](#endpoints)
  - [Health Check](#health-check)
  - [Process Management](#process-management)
  - [Census Management](#census-management)
  - [Vote Management](#vote-management)
  - [Vote Status](#vote-status)
  - [Ballot Proof Information](#ballot-proof-information)
  - [Worker Management](#worker-management)
  - [Sequencer Statistics](#sequencer-statistics)

## Base URL

All endpoints are relative to the base URL of the API server.

## Response Format

All responses are returned as JSON objects. Successful responses will have a 200 OK status code unless otherwise specified. Error responses will include an error message and code.

## Error Handling

API errors are returned with appropriate HTTP status codes and a JSON body with error details:

```json
{
  "error": "error message",
  "code": 40001
}
```

### Error Codes

| Code  | HTTP Status | Description                                |
|-------|-------------|--------------------------------------------|
| 40001 | 404         | Resource not found                         |
| 40004 | 400         | Malformed JSON body                        |
| 40005 | 400         | Invalid signature                          |
| 40006 | 400         | Malformed process ID                       |
| 40007 | 404         | Process not found                          |
| 40008 | 400         | Invalid census proof                       |
| 40009 | 400         | Invalid ballot proof                       |
| 40010 | 400         | Invalid census ID                          |
| 40011 | 404         | Census not found                           |
| 40012 | 400         | Key length exceeded                        |
| 40013 | 400         | Invalid ballot inputs hash                 |
| 40014 | 403         | Unauthorized                               |
| 40015 | 400         | Malformed parameter                        |
| 40017 | 400         | Malformed address                          |
| 40018 | 400         | Ballot already submitted                   |
| 40019 | 409         | Ballot is already processing               |
| 40020 | 400         | Process is not accepting votes             |
| 40021 | 400         | Not supported chain Id                     |
| 40022 | 400         | Worker not available                       |
| 40023 | 400         | Malformed worker info                      |
| 40024 | 403         | invalid worker authentication token        |
| 40025 | 403         | expired worker authentication token        |
| 40026 | 404         | worker not found                           |
| 50001 | 500         | Marshaling (server-side) JSON failed       |
| 50002 | 500         | Internal server error                      |

## Endpoints

### Health Check

#### GET /ping

Simple health check endpoint to verify the API server is running.

**Response**: Empty response with HTTP 200 OK status

**Errors**:
- None

### Information

#### GET /info

Returns information needed by the client to generate a ballot zkSNARK proof, including circuit URLs, hashes, and smart contract addresses.

**Response Body**:
```json
{
  "circuitUrl": "string",
  "circuitHash": "hexString",
  "provingKeyUrl": "string",
  "provingKeyHash": "hexString",
  "verificationKeyUrl": "string",
  "verificationKeyHash": "hexString",
  "ballotProofWasmHelperUrl": "string",
  "ballotProofWasmHelperHash": "hexString",
  "ballotProofWasmHelperExecJsUrl": "string",
  "ballotProofWasmHelperExecJsHash": "hexString",
  "contracts": {
    "process": "address",
    "organization": "address",
    "stateTransitionVerifier": "address",
    "resultsVerifier": "address",
  },
  "network": { 
    "sepolia": 11155111
  },
  "sequencerAddress": "hexString"
}
```

**Errors**:
- 50001: Marshaling server JSON failed
- 50002: Internal server error (invalid network configuration)


### Process Management

#### POST /processes

Creates a new voting process setup and returns it.

The signature is the byte representation of the string `I am creating a new voting process for the davinci.vote protocol identified with id {processId}`,
where `processId` is the hexadecimal string (without `0x` prefix) of the process identifier fetch on the smart contract.

The `censusOrigin` specifies the origin type of the census used in the request. This attribute allows the API to determine how the census data should be processed or verified.
It can be:
 - `1` –> CensusOriginMerkleTree: Indicates that the census is derived from a Merkle Tree structure. This is typically used when the census data is represented as cryptographic proofs for membership verification.
 - `2` -> CensusOriginCSP: Indicates that the census is provided by a Credential Service Providers (CSP). This origin is commonly used when the census data is managed by an external trusted provider.



**Request Body**:
```json
{
  "processId": "hexBytes",
  "censusOrigin": "number",
  "censusRoot": "hexBytes",
  "ballotMode": {
    "numFields": "number",
    "maxValue": "bigintStr",
    "minValue": "bigintStr",
    "uniqueValues": "boolean",
    "costFromWeight": "boolean",
    "costExponent": "number",
    "maxValueSum": "bigintStr",
    "minValueSum": "bigintStr"
  },
  "signature": "hexBytes"
}
```

**Response Body**:
```json
{
  "processId": "hexBytes",
  "encryptionPubKey": ["bigintStr", "bigintStr"],
  "stateRoot": "hexBytes",
  "ballotMode": {
    "numFields": "number",
    "maxValue": "bigintStr",
    "minValue": "bigintStr",
    "uniqueValues": "boolean",
    "costFromWeight": "boolean",
    "costExponent": "number",
    "maxValueSum": "bigintStr",
    "minValueSum": "bigintStr"
  }
}
```

**Errors**:
- 40004: Malformed JSON body
- 40005: Invalid signature
- 50002: Internal server error

### Metadata Management

#### POST /metadata

Sets metadata for a voting process.

**Request Body**:
```json
{
  "title": {
    "languageCode": "string" // Language code as key, text as value. Example: {"en": "Election", "es": "Elección"}
  },
  "description": {
    "languageCode": "string" // Language code as key, text as value
  },
  "media": {
    "header": "string", // URL to header image
    "logo": "string"    // URL to logo image
  },
  "questions": [
    {
      "title": {
        "languageCode": "string" // Language code as key, text as value
      },
      "description": {
        "languageCode": "string" // Language code as key, text as value
      },
      "choices": [
        {
          "title": {
            "languageCode": "string" // Language code as key, text as value
          },
          "value": "number",
          "meta": {
            "key": "string" // Free-form key-value pairs, can contain any valid JSON
          }
        }
      ],
      "meta": {
        "key": "string" // Free-form key-value pairs, can contain any valid JSON
      }
    }
  ],
  "type": {
    "name": "string",
    "properties": {
      "key": "string" // Free-form key-value pairs, can contain any valid JSON
    }
  },
  "version": "string",
  "meta": {
    "key": "string" // Free-form key-value pairs, can contain any valid JSON
  }
}
```

**Response Body**:
```json
{
  "hash": "hexBytes"
}
```

**Errors**:
- 40004: Malformed JSON body
- 50001: Marshaling server JSON failed
- 50002: Internal server error

#### GET /metadata/{metadataHash}

Retrieves metadata by its hash.

**URL Parameters**:
- metadataHash: Metadata hash in hexadecimal format

**Response Body**:
Returns the complete metadata object as per the POST request format.

**Errors**:
- 40001: Resource not found
- 40004: Malformed parameter
- 50002: Internal server error

#### GET /processes

Lists all available voting process IDs.

**Response Body**:
```json
{
  "processes": [
    "hexBytes",
    "hexBytes",
    "hexBytes"
  ]
}
```

**Errors**:
- 50002: Internal server error

#### GET /processes/{processId}

Gets information about an existing voting process. It must exist in the smart contract.

**URL Parameters**:
- processId: Process ID in hexadecimal format

**Response Body**:
```json
{
  "id": "hexBytes",
  "status": "number",
  "organizationId": "address",
  "encryptionKey": {
    "x": "bigintStr",
    "y": "bigintStr"
  },
  "stateRoot": "hexBytes",
  "result": ["bigintStr"],
  "startTime": "timestamp",
  "duration": "duration",
  "metadataURI": "string", // URI/URL to fetch the process metadata
  "ballotMode": {
    "numFields": "number",
    "maxValue": "bigintStr",
    "minValue": "bigintStr",
    "uniqueValues": "boolean",
    "costFromWeight": "boolean",
    "costExponent": "number",
    "maxValueSum": "bigintStr",
    "minValueSum": "bigintStr"
  },
  "census": {
    "censusOrigin": "number",
    "maxVotes": "bigintStr",
    "censusRoot": "hexBytes",
    "censusURI": "string"
  },
  "voteCount": "bigintStr", // Total number of votes cast in the process
  "voteOverwrittenCount": "bigintStr", // Number of times voters changed their vote
  "isAcceptingVotes": "boolean", // Whether the Sequencer is currently accepting votes for this process
  "sequencerStats": { // Stats about the Sequencer runing the API (not the whole network)
    "stateTransitionCount": "number", // Total number of state transitions performed
    "lastStateTransitionDate": "date", // Date of the most recent state transition
    "settledStateTransitionCount": "number", // Number of state transitions settled to the Ethereum blockchain
    "aggregatedVotesCount": "number", // Total number of votes that have been aggregated into batches
    "verifiedVotesCount": "number", // Total number of votes that have been cryptographically verified
    "pendingVotesCount": "number", // Number of votes waiting to be processed
    "currentBatchSize": "number", // Number of votes in the current batch being prepared
    "lastBatchSize": "number" // Number of votes in the last completed batch
  }
}
```

**Errors**:
- 40006: Malformed process ID
- 40007: Process not found
- 50002: Internal server error

### Census Management

#### POST /censuses

Creates a new census.

**Response Body**:
```json
{
  "census": "uuid"
}
```

**Errors**:
- 50002: Internal server error

#### POST /censuses/{censusId}/participants

Adds participants to an existing census.

**URL Parameters**:
- censusId: Census UUID

**Request Body**:
```json
{
  "participants": [
    {
      "key": "hexBytes", // if more than 20 bytes, it is hashed and truncated
      "weight": "bigintStr" // optional, defaults to 1
    }
  ]
}
```

**Response**: Empty response with HTTP 200 OK status

**Errors**:
- 40004: Malformed JSON body
- 40010: Invalid census ID
- 40011: Census not found
- 50002: Internal server error

#### GET /censuses/{censusId}/participants

Gets the list of participants in a census.

**URL Parameters**:
- censusId: Census UUID

**Response Body**:
```json
{
  "participants": [
    {
      "key": "hexBytes",
      "weight": "bigintStr"
    }
  ]
}
```

**Errors**:
- 40004: Malformed JSON body
- 40010: Invalid census ID
- 50002: Internal server error

#### GET /censuses/{censusId}/root

Gets the Merkle root of a census.

**URL Parameters**:
- censusId: Census UUID

**Response Body**:
```json
{
  "root": "hexBytes"
}
```

**Errors**:
- 40010: Invalid census ID
- 50002: Internal server error

#### GET /censuses/{censusId}/size

Gets the number of participants in a census.

**URL Parameters**:
- censusId: Census UUID or census merkle root (hex encoded)

**Response Body**:
```json
{
  "size": "number"
}
```

**Errors**:
- 40010: Invalid census ID
- 50002: Internal server error

#### DELETE /censuses/{censusId}

Deletes a census.

**URL Parameters**:
- censusId: Census UUID

**Response**: Empty response with HTTP 200 OK status

**Errors**:
- 40010: Invalid census ID
- 50002: Internal server error

#### GET /censuses/{censusRoot}/proof

Gets a Merkle proof for a participant in a census.

**URL Parameters**:
- censusRoot: Census merkle root (hex encoded)

**Query Parameters**:
- key: Participant key (hex encoded)

**Response Body**:
```json
{
  "root": "hexBytes",
  "address": "hexBytes",
  "weight": "bigintStr",
  "censusOrigin": 1,        // 1 for merkle proofs, 2 for csp proofs
  "value": "hexBytes",      // merkle proof: the weight encoded to hexBytes
  "siblings": "hexBytes",   // merkle proof: encoded siblings path to find the leaf
  "processId": "hexBytes",  // csp proof: the process id signed with the address
  "publicKey": "hexBytes",  // csp proof: the public key of the csp
  "signature": "hexBytes",  // csp proof: the signature that proofs that the voter is in the census
}
```

**Errors**:
- 40001: Resource not found
- 40004: Malformed body (invalid key)
- 40010: Invalid census ID
- 50002: Internal server error

### Vote Management

#### POST /votes

Register a new vote for a voting process.

**Request Body**:
```json
{
  "processId": "hexBytes",
  "censusProof": {
    "root": "hexBytes",
    "address": "hexBytes",
    "weight": "bigintStr",
    "censusOrigin": 1,        // 1 for merkle proofs, 2 for csp proofs
    "value": "hexBytes",      // merkle proof: the weight encoded to hexBytes
    "siblings": "hexBytes",   // merkle proof: encoded siblings path to find the leaf
    "processId": "hexBytes",  // csp proof: the process id signed with the address
    "publicKey": "hexBytes",  // csp proof: the public key of the csp
    "signature": "hexBytes",  // csp proof: the signature that proofs that the voter is in the census
  },
  "ballot": {
    "curveType": "string",
    "ciphertexts": [
      {
        "c1": ["bigintStrX","bigintStrY"],
        "c2": ["bigintStrX","bigintStrY"]
      },
      {
        "c1": ["bigintStrX","bigintStrY"],
        "c2": ["bigintStrX","bigintStrY"]
      }
    ...
    ]
  },
  "ballotProof": {
    "pi_a": ["string"],
    "pi_b": [["string"]],
    "pi_c": ["string"],
    "protocol": "string"
  },
  "ballotInputsHash": "bigintStr",
  "publicKey": "hexBytes",
  "signature": "hexBytes",
  "voteId": "hexBytes",
}
```

**Errors**:
- 40001: Resource not found (process not found)
- 40004: Malformed JSON body
- 40005: Invalid signature
- 40008: Invalid census proof
- 40009: Invalid ballot proof
- 40013: Invalid ballot inputs hash
- 40018: Ballot already submitted
- 40019: Ballot is already processing
- 40020: Process is not accepting votes
- 50002: Internal server error

#### GET /votes/{processId}/address/{address}

Gets a vote by its address for a specific process.

**URL Parameters**:
- processId: Process ID in hexadecimal format
- address: address value as a hexdecimal string

**Response Body**:
Returns the encrypted ballot if found.

**Errors**:
- 40001: Resource not found
- 40006: Malformed process ID
- 40017: Malformed address
- 40007: Process not found
- 50002: Internal server error

### Vote Status

#### GET /votes/{processId}/voteId/{voteId}

Gets the status of a specific vote within a voting process.

**URL Parameters**:
- processId: Process ID in hexadecimal format
- voteId: Vote ID in hexadecimal format

**Response Body**:
```json
{
  "status": "string"
}
```

The status can be one of the following values:
- "pending": The vote has been submitted but not yet verified
- "verified": The vote has been verified
- "aggregated": The vote has been included in an aggregated batch
- "processed": The vote has been included in a state transition batch
- "settled": The vote has been settled on the Ethereum blockchain
- "error": An error occurred during processing

**Errors**:
- 40001: Resource not found (vote not found)
- 40006: Malformed process ID
- 40004: Malformed vote ID
- 50002: Internal server error


### Sequencer Statistics

#### GET /sequencer/stats

Gets overall statistics for the sequencer service, including aggregated metrics across all active processes.

**Response Body**:
```json
{
  "activeProcesses": "number",
  "pendingVotes": "number",
  "verifiedVotes": "number",
  "aggregatedVotes": "number",
  "stateTransitions": "number",
  "settledStateTransitions": "number",
  "lastStateTransitionDate": "date"
}
```

**Errors**:
- 50001: Marshaling server JSON failed
- 50002: Internal server error

#### GET /sequencer/workers

Gets a list of all worker nodes with their statistics.

**Response Body**:
```json
{
  "workers": [
    {
      "name": "worker-name-1",
      "successCount": 150,
      "failedCount": 5
    },
    {
      "name": "**************************************7C",
      "successCount": 200,
      "failedCount": 2
    }
  ]
}
```

**Errors**:
- 50002: Internal server error
