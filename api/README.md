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
| 40016 | 400         | Malformed nullifier                        |
| 40017 | 400         | Malformed address                          |
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
    "results": "address"
  }
}
```

**Errors**:
- 50001: Marshaling server JSON failed
- 50002: Internal server error (invalid network configuration)


### Process Management

#### POST /processes

Creates a new voting process setup and returns it. The process is not permanently stored.

**Request Body**:
```json
{
  "censusRoot": "hexBytes",
  "ballotMode": {
    "maxCount": "number",
    "maxValue": "bigintStr",
    "minValue": "bigintStr",
    "forceUniqueness": "boolean",
    "costFromWeight": "boolean",
    "costExponent": "number",
    "maxTotalCost": "bigintStr",
    "minTotalCost": "bigintStr"
  },
  "nonce": "number",
  "chainId": "number",
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
    "maxCount": "number",
    "maxValue": "bigintStr",
    "minValue": "bigintStr",
    "forceUniqueness": "boolean",
    "costFromWeight": "boolean",
    "costExponent": "number",
    "maxTotalCost": "bigintStr",
    "minTotalCost": "bigintStr"
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
    "maxCount": "number",
    "maxValue": "bigintStr",
    "minValue": "bigintStr",
    "forceUniqueness": "boolean",
    "costFromWeight": "boolean",
    "costExponent": "number",
    "maxTotalCost": "bigintStr",
    "minTotalCost": "bigintStr"
  },
  "census": {
    "censusOrigin": "number",
    "maxVotes": "bigintStr",
    "censusRoot": "hexBytes",
    "censusURI": "string"
  },
  "voteCount": "bigintStr", // Total number of votes cast in the process
  "voteOverwriteCount": "bigintStr", // Number of times voters changed their vote
  "isFinalized": "boolean", // Whether the voting process has been finalized and results are available
  "isAcceptingVotes": "boolean", // Whether the Sequencer is currently accepting votes for this process

  "sequencerStats": { // Stats about the Sequencer runing the API (not the whole network)
    "stateTransitionCount": "number", // Total number of state transitions performed
    "lastStateTransitionDate": "date", // Date of the most recent state transition
    "uploadedStateTransitionCount": "number", // Number of state transitions uploaded to the blockchain
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
  "key": "hexBytes",
  "value": "hexBytes",
  "siblings": "hexBytes",
  "weight": "bigintStr" // the value transformed to bigInt
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
  "commitment": "bigintStr",
  "nullifier": "bigintStr",
  "censusProof": {
    "root": "hexBytes",
    "key": "hexBytes",
    "value": "hexBytes",
    "siblings": "hexBytes",
    "weight": "bigintStr"
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
  "signature": "hexBytes"
}
```

**Response Body**:
```json
{
  "voteId": "hexBytes"
}
```

**Errors**:
- 40001: Resource not found (process not found)
- 40004: Malformed JSON body
- 40005: Invalid signature
- 40008: Invalid census proof
- 40009: Invalid ballot proof
- 50002: Internal server error

#### GET /votes/{processId}/nullifier/{nullifier}

Gets a vote by its nullifier for a specific process.

**URL Parameters**:
- processId: Process ID in hexadecimal format
- nullifier: Nullifier value as a decimal string (big.Int representation)

**Response Body**:
Returns the encrypted ballot if found.

**Errors**:
- 40001: Resource not found
- 40006: Malformed process ID
- 40016: Malformed nullifier
- 40007: Process not found
- 50002: Internal server error

#### GET /votes/{processId}/address/{address}

Checks if an address has already voted in a specific process.

**URL Parameters**:
- processId: Process ID in hexadecimal format
- address: Ethereum address to check (hex format)

**Response**:
- 200 OK if the address has already voted
- 404 Not Found if the address has not voted

**Errors**:
- 40001: Resource not found (address has not voted)
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

### Worker Management

#### GET /workers

Gets a list of all worker nodes with their statistics.

**URL Parameters**:
- uuid: Worker authentication UUID (derived from worker seed)

**Response Body**:
```json
{
  "workers": [
    {
      "address": "0x742d35Cc6C82C3e76E8B8c9b4aE3F4F7E5A8c6D2",
      "successCount": 150,
      "failedCount": 5
    },
    {
      "address": "0x8F4A7B2C1D6E9F3A5B8C2E7F1A4D6C9E2B5A8F7C",
      "successCount": 200,
      "failedCount": 2
    }
  ]
}
```

**Errors**:
- 401: Unauthorized (invalid UUID)
- 50002: Internal server error
