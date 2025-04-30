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
  - [Ballot Proof Information](#ballot-proof-information)

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
  "ballotProofWasmHelperExecJs": "string",
  "ballotProofWasmHelperExecHash": "hexString",
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
  "stateRoot": "hexBytes"
}
```

**Errors**:
- 40004: Malformed JSON body
- 40005: Invalid signature
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
  "metadataURI": "string",
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
  "metadata": {
    "title": {
      "languageCode": "string"
    },
    "description": {
      "languageCode": "string"
    },
    "media": {
      "header": "string",
      "logo": "string"
    },
    "questions": [
      {
        "title": {
          "languageCode": "string"
        },
        "description": {
          "languageCode": "string"
        },
        "choices": [
          {
            "title": {
              "languageCode": "string"
            },
            "value": "number",
            "meta": {
              "key": "string"
            }
          }
        ],
        "meta": {,
          "key": "string"
        }
      }
    ],
    "processType": {
      "name": "string",
      "properties": {
        "key": "string"
      }
    }
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
        "c1": {
          "x": "bigintStr",
          "y": "bigintStr"
        },
        "c2": {
          "x": "bigintStr",
          "y": "bigintStr"
        }
      }
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

**Response**: Empty response with HTTP 200 OK status

**Errors**:
- 40001: Resource not found (process not found)
- 40004: Malformed JSON body
- 40005: Invalid signature
- 40008: Invalid census proof
- 40009: Invalid ballot proof
- 50002: Internal server error

