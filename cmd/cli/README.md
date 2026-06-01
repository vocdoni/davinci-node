# Davinci CLI

`davinci-cli` is a command-line tool for interacting with the **DAVINCI** decentralized voting platform. It manages the full lifecycle of onchain voting processes — creation, vote submission, and process stop — across multiple EVM networks. The CLI supports several census origins (predefined Merkle trees, dynamically generated voter sets via Census3, and CSP-based censuses) and handles all the plumbing: census publishing, proof generation, blockchain transactions, and sequencer coordination.

## Table of Contents

- [Use](#use)
  - [Available flags](#available-flags)
- [Process config file](#process-config-file)
  - [ProcessID](#processid)
  - [Network \`chainID\`](#network-chainid)
  - [Census](#census)
    - [Predefined Merkle-Tree based census](#predefined-merkle-tree-based-census)
    - [Merkle-Tree based created by the CLI](#merkle-tree-based-created-by-the-cli)
      - [With random voters](#with-random-voters)
      - [With predefined voters](#with-predefined-voters)
    - [CSP based census](#csp-based-census)
  - [Full \`json\` example](#full-json-example)

## Use

```bash
go run ./cmd/cli --web3.privkey <pk> --processConfig cmd/cli/process-config.example.json
```

### Available flags

- `--processConfig` or `--p` [required]: Path to json process template. For example [`cmd/cli/process-config.example.json`](./process-config.example.json)
- `--web3.privkey` or `--k` [required]: Private key to be used to call to the blockchain network, for example to create and manage the voting process. It should have funds of every network used.
- `--web3.rpc` or `--r` [optional]: The list (comma-separated) of RPC endpoints (they can be multinetwork). If no RPCs are provided, they will be queried to ChainList.
- `--web3.processRegistryContract` [optional]: A list of chainID:0xAddress of ProcessRegistry contracts to be used, by default, queried to the sequencer.
- `--sequencer` or `--s` [optional]: The DAVINCI sequencer server endpoint. Default `https://sequencer5.davinci.vote`.
- `--census3` [optional]: The Census3 service endpoint, where the non-onchain censuses are created and published to been available for the sequencer. Default `https://c3-dev.davinci.vote`.
- `--globalTimeout` [optional]: Timeout for the whole CLI. Default `20m`.
- `--logLevel` [optional]: Default `INFO`.
- `--action` or `--a` [optional]: By default `"all"`, available `"create"`, `"vote"`, `"stop"` or `"all"`.

### Process config file

#### ProcessID

Required to submit votes and stop a process. No required to create a process or end to end flow (create process - submit votes - stop it).

```json
{
    "processID": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
    // ...
}
```

#### Network `chainID`

Required. The `chainID` where the process lives.

```json
{
    // ...
    "chainID": 11155111
}
```

#### Census

##### Predefined Merkle-Tree based census

Used as-is to create the process in the network. The sequencer will import the census participants from its URI to be able to generate census proofs during the process. The census root should match with the computed after import it.

It can be defined as: 
- `merkle_tree_offchain_static_v1`: Created once offchain and used as-is.
- `merkle_tree_offchain_dynamic_v1`: Created offchain, updatable during the time. The sequencer uses the URI to keep it updated locally.
- `merkle_tree_onchain_dynamic_v1`: Created onchain. The sequencer uses the Contract Address to keep it synced with the network.

```json
{
    // ...
    "censusConfig": {
        "censusOrigin": "merkle_tree_offchain_static_v1",
        "censusRoot": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
        "censusURI": "https://my-census.uri",
        "contractAddress": "0xabcdef1234567890abcdef1234567890abcdef12"
    }
}
```

##### Merkle-Tree based created by the CLI

###### With random voters

The CLI will generate as many voters as defined by the JSON with the desired weight, then it will create and publish the census using the Census3 service.

```json
{
    // ...
    "censusConfig": {
        "censusOrigin": "merkle_tree_offchain_static_v1", // or "merkle_tree_offchain_dynamic_v1"
    },
    "votersConfig": {
        "numVoters": 10,
        "defaultWeight": "10"
    }
}
```

###### With predefined voters

The CLI will create and publish the census using the Census3 service.

```json
{
    // ...
    "censusConfig": {
        "censusOrigin": "merkle_tree_offchain_static_v1", // or "merkle_tree_offchain_dynamic_v1"
    },
    "votersConfig": {
        "votersInfo": [
            {
                "address": "0xabcdef1234567890abcdef1234567890abcdef12",
                "weight": "10"
            },
            {
                "address": "0x1234567890abcdef1234567890abcdef12345678",
                "weight": "10"
            },
        ]
    }
}
```

##### CSP based census

The CLI will create a CSP service locally with the seed provided to generate proofs during voting process.

```json
{
    // ...
    "cspConfig": {
        "censusOrigin": "csp_eddsa_babyjubjub_v1",
        "seed": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
    },
}
```


#### Full `json` example
```json
{
    "processID": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef12345678",
    "chainID": 11155111,
    "censusConfig": {
        "censusOrigin": "merkle_tree_offchain_static_v1",
        "censusRoot": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
        "censusURI": "https://my-census.uri",
        "contractAddress": "0xabcdef1234567890abcdef1234567890abcdef12"
    },
    "cspConfig": {
        "censusOrigin": "csp_eddsa_babyjubjub_v1",
        "seed": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
    },
    "votersConfig": {
        "votersInfo": [
            {
                "address": "0xabcdef1234567890abcdef1234567890abcdef12",
                "weight": "10",
                "privKey": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
            },
            {
                "address": "0x1234567890abcdef1234567890abcdef12345678",
                "weight": "10",
                "privKey": "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
            },
        ],
        "numVoters": 10,
        "defaultWeight": "10"
    },
    "ballotMode": {
        "numFields": 6,
        "groupSize": 1,
        "uniqueValues": false,
        "costExponent": 1,
        "maxValue": 10,
        "minValue": 0,
        "maxValueSum": 60,
        "minValueSum": 0
    },
    "metadata": { // optional
        "title": {
            "en": "Title example"
        },
        "description": {
            "en": "Description example"
        },
        "media": {
            "header": "http://img.com/header.png",
            "logo": "http://img.com/logo.png"
        },
        "questions": [
            {
                "title": {
                    "en": "Question title example"
                },
                "description": {
                    "en": "Question description example"
                },
                "choices": [
                    {
                        "title": {
                            "en": "Choice title example"
                        },
                        "value": 1,
                        "meta": {
                            "key": "value"
                        }
                    }
                ],
                "meta": {
                    "key": "value"
                }
            }
        ],
        "type": {
            "name": "single-choice",
            "properties": {
                "key": "value"
            }
        },
        "version": "v1",
        "meta": {
            "key": "value"
        }
    },
    "maxVoters": 10,
    "startTime": "2006-01-02T15:04:05Z07:00",  // optional, by default "now + 1m"
    "duration": "1h"  // optional, by default "10m"
}
```