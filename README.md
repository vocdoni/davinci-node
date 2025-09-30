# DAVINCI Node

{**D**ecentralized **A**utonomous **V**ote **I**ntegrity **N**etwork with **C**ryptographic **I**nference}

Davinci-Node is the main implementation of the [davinci.vote](https://davinci.vote) protocol. A zkSNARK-based voting network that processes encrypted ballots and generates cryptographic proofs for decentralized voting. 

📖 **Read the full technical whitepaper:** [whitepaper.vocdoni.io](https://whitepaper.vocdoni.io)

## Table of Contents

- [🚀 Quick Start](#-quick-start)
- [🔧 Run a Sequencer](#-run-a-sequencer)
  - [Basic Setup](#basic-setup)
  - [HTTPS with Let's Encrypt](#https-with-lets-encrypt)
  - [Enable Workers API](#enable-workers-api)
  - [Dashboard Web UI](#dashboard-web-ui)
  - [Command Line Options](#command-line-options)
- [⚡ Run a Worker Node](#-run-a-worker-node)
  - [Update your worker](#update-your-worker)
- [🧑‍🧑‍🧒‍🧒 Run a CSP: Credential Service Providers](#-run-a-csp-credentials-service-provider)
- [📚 Additional Resources](#-additional-resources)

## 🚀 Quick Start

The fastest way to get started is by running a Sequencer node using Docker.

## 🔧 Run a Sequencer

The Sequencer is a specialized component designed to handle the voting process using zero-knowledge proof mechanisms. It ensures that all votes related to this process are validated and sequenced. The Sequencers periodically commit the state of the voting process to Ethereum.

### Basic Setup

1. **Clone the repository:**
   ```bash
   git clone https://github.com/vocdoni/davinci-node.git 
   cd davinci-node
   ```

2. **Copy the example ENV file:**
   ```bash
   cp .env.example .env
   ```

3. **Configure the environment variables** in the `.env` file:
   ```bash
   DAVINCI_WEB3_PRIVKEY=<hex private key with funds> # currently Sepolia ETH
   DAVINCI_WEB3_NETWORK=sepolia # for Sepolia
   DAVINCI_API_WORKERSEED=someRandomSeed # just provide some entropy to generate a UUID
   ```

4. **Run the docker container:**
   ```bash
   docker compose up -d sequencer
   ```

5. **Enable auto-updates (recommended):**
   ```bash
   docker compose up -d watchtower
   ```

### API Access

The node exposes a HTTP/REST API. See the full documentation at [api/README.md](https://github.com/vocdoni/davinci-node/tree/main/api).

**Example API query:**
```bash
curl -s http://localhost:9090/sequencer/stats
```

**Response:**
```json
{
  "verifiedVotes": 140,
  "aggregatedVotes": 140,
  "stateTransitions": 6,
  "settledStateTransitions": 5,
  "lastStateTransitionDate": "2025-06-12T10:09:48Z",
  "activeProcesses": 0,
  "pendingVotes": 0
}
```

### HTTPS with Let's Encrypt

To run with a custom domain name and an auto-generated TLS certificate, add the following ENV var to `.env` file:

```bash
DOMAIN=mydomain.com
```

And execute the docker compose with `--profile=prod`. This is launch all required services (including watchtower).

```bash
docker compose --profile=prod up -d
```

### Enable Workers API

Davinci-Node supports distributed proving through a worker system that allows multiple nodes to collaborate in processing zkSNARK proofs. It can operate in two modes:

1. **Master Mode**: A complete sequencer that processes votes, manages the ballot queue, and can optionally distribute zkSNARK proving workload to worker nodes.
2. **Worker Mode**: A lightweight node that only handles zkSNARK proof generation for ballots assigned by a master node.

The worker system enables distributed zkSNARK proving, allowing the computational workload to be distributed across multiple nodes.

#### Authentication System

Workers authenticate using a UUID-based system:
- Master generates a UUID from a configurable seed using `hash(UrlSeed)`
- Workers must know the correct master URL including the UUID
- Workers are expected to provide an Ethereum address so the Master node keeps track of the success/failed jobs for each worker (enables potential payouts)

#### Getting the Worker URL

The worker master URL (including the secret UUID) can be fetched from the logs. Search for a message like this:

> INF [...] > worker API enabled url=/workers/8d969eef-6eca-d3c2-9a3a-629280e686cf

Then the full URL to share with the Worker nodes would be:
```
https://mydomain.com/workers/8d969eef-6eca-d3c2-9a3a-629280e686cf
```

> 💡 **Tip:** See the [Worker Node setup section](#-run-a-worker-node) for detailed worker configuration.


### Dashboard Web UI

The sequencer includes a web UI dashboard accessible by default at `http://localhost:9090/app`

The UI provides:
- Smart contract addresses with block explorer links
- Process list with statistics and real-time updates
- Detailed process information including voting results
- Filtering and sorting capabilities

#### Configuration

The Web UI supports multiple configuration methods:

1. **Environment Variables**: Set `SEQUENCER_API_URL` and `BLOCK_EXPLORER_URL` when running the container
2. **In-App**: Use the input field at the top of the dashboard to change the API URL on the fly

If using `SEQUENCER_API_URL=https://mydomain.com`, the sequencer needs to be built (instead of using remote images),
so `docker compose build; docker compose up -d sequencer` is necessary.

### Command Line Options

For development or custom deployments, you can run the sequencer directly with Go:

```bash
go run ./cmd/davinci-sequencer -h
```

#### Available Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--web3.privkey` | `-k` | | Private key for Ethereum account (required for master) |
| `--web3.network` | `-n` | `sepolia` | Network to use (sepolia, mainnet, etc.) |
| `--web3.rpc` | `-r` | | Custom RPC endpoints (comma-separated) |
| `--api.host` | `-h` | `0.0.0.0` | API host address |
| `--api.port` | `-p` | `9090` | API port number |
| `--api.workerSeed` | none | | URL seed for worker authentication |
| `--batch.time` | `-b` | `5m` | Batch processing time window |
| `--log.level` | `-l` | `info` | Log level (debug, info, warn, error) |
| `--log.output` | `-o` | `stdout` | Log output destination |
| `--datadir` | `-d` | `~/.davinci` | Data directory path |
| `--worker.sequencerURL` | `-w` | | Sequencer URL for worker mode |
| `--worker.address` | `-a` | | Worker Ethereum address |
| `--worker.authtoken` | none | | Worker authtoken for worker mode |
| `--worker.timeout` | none | `1m` | Worker job timeout duration |

## ⚡ Run a Worker Node

Worker nodes are lightweight components that handle zkSNARK proof generation for ballots assigned by a master sequencer node. This enables distributed proving and helps scale the network.

### Setup Steps

0. **Create a Worker Authtoken**
   Go to [Davinci Worker Registry](https://vocdoni.github.io/davinci-workers-registry/) webapp to get your token. Ensures that the account used to create it matches with the worker address.

1. **Clone the repository:**
   ```bash
   git clone https://github.com/vocdoni/davinci-node.git 
   cd davinci-node
   ```

2. **Copy the example ENV file:**
   ```bash
   cp .env.example .env
   ```

3. **Configure worker-specific variables** in the `.env` file:
   ```bash
   DAVINCI_WORKER_SEQUENCERURL="http://sequencer-host:9090/workers/<UUID>"
   DAVINCI_WORKER_AUTHTOKEN="<generated_worker_authtoken"
   DAVINCI_WORKER_ADDRESS="0x1111122222333334444455555666667777788888"
   DAVINCI_WORKER_NAME="my-awesome-davinci-worker"
   ```

4. **Start the worker container:**
   ```bash
   docker compose up -d sequencer
   ```

#### Update your worker

> ℹ️ If you have a `watchtower` instance running, it your worker should update itself automatically.

1. **Pull the latest version from the repository:**
   ```bashde
   cd davinci-node
   git pull origin main
   ```

2. **Rebuild docker images:**
   ```bash
   docker compose pull
   ```

3. **Start the worker again:**
   ```bash
   docker compose up -d --force-recreate sequencer
   ```

### Configuration Notes

> ⚠️ **Important:** The Master URL (including the UUID) must be provided by the owner of the Master Sequencer node. See the [Workers API section](#enable-workers-api) for details on how to obtain this URL.

> 💡 **Note:** The Ethereum address can be any valid address. It's used for accounting purposes and tracking success/failed jobs, but does not need to own any funds.

## 🧑‍🧑‍🧒‍🧒 Run a CSP: Credentials Service Provider

A **Credential Service Provider (CSP)** allows organizations to validate users manually and based off of any arbitrary criteria. Rather than a static census published before-hand, CSP census allows each user to be evaluated for voting eligibility individually, throughout the duration of the voting process.

In order to prove they are a member of the census, a voter needs to retrieve a certificate of eligibility from the CSP for that process. The CSP first verifies the user's validity and then provides this certificate (proof) by signing the voter address and the process ID.

### Supported Census Origins

The sequencers only supports the following census origin, that may be used by the CSP's to generate valid proofs for the voters.

| Census Origin Variable | Value | Description |
|:---|:---:|:---|
| `CensusOriginCSPEdDSABLS12377` | `2` | EdDSA signatures over the BLS12-377 curve |

### What a CSP Does

- **Create census proofs** for specific participants using a deterministic seed.
- **Verify census proofs** to ensure their validity and integrity.
- **Expose census origin and root** for external systems to validate the source and version of the census.

### Available Methods

The `crypto/csp` package provides two helpers functions:

- `New(origin types.CensusOrigin, seed []byte) (CSP, error)` – Creates a new CSP instance for the specified origin.
- `VerifyCensusProof(proof *types.CensusProof) error` – Verifies a proof by creating an appropriate CSP automatically.

The `CSP` interface has the following methods:

- `SetSeed(seed []byte) error` – Sets the cryptographic seed used by the CSP.
- `CensusOrigin() types.CensusOrigin` – Returns the type of census origin (e.g., `CensusOriginCSPEdDSABLS12377`).
- `CensusRoot() types.HexBytes` – Returns the census root hash.
- `GenerateProof(processID *types.ProcessID, address common.Address) (*types.CensusProof, error)` – Generates a cryptographic proof for a given participant.
- `VerifyProof(proof *types.CensusProof) error` – Verifies that a given proof is valid for the configured CSP.

### Example Usage

#### Go example

```go
package main

import (
	"fmt"
	"math/rand"

	"github.com/ethereum/go-ethereum/common"
	"github.com/vocdoni/davinci-node/crypto/csp"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/util"
)

func main() {
	// Select the CSP origin and provide a seed
	origin := types.CensusOriginCSPEdDSABLS12377
	seed := []byte("example_seed")

	// Create a new CSP instance
	c, err := csp.New(origin, seed)
	if err != nil {
		panic(fmt.Sprintf("failed to create CSP: %v", err))
	}

	// Mock process identifier
	processID := &types.ProcessID{
		Address: common.BytesToAddress(util.RandomBytes(20)),
		ChainID: 1,
		Nonce:   rand.Uint64(),
	}

	// Voter address
	voter := common.BytesToAddress(util.RandomBytes(20))

	// Generate a census proof for the voter
	proof, err := c.GenerateProof(processID, voter)
	if err != nil {
		panic(fmt.Sprintf("failed to generate proof: %v", err))
	}

	// Verify the generated proof
	if err := c.VerifyProof(proof); err != nil {
		panic(fmt.Sprintf("failed to verify proof: %v", err))
	}

	fmt.Println("Census proof verified successfully!")
}
```

#### Node.js and JavaScript example

Take a look to `davinci-crypto` WebAssembly [here](./cmd/davincicrypto-wasm/README.md).

```html
<script src="wasm_exec.js"></script>
<script>
   const go = new Go();
   WebAssembly.instantiateStreaming(fetch('davinci_crypto.wasm'), go.importObject)
   .then(result => go.run(result.instance))
   .then(() => {
      const censusOrigin = 2;
      const privKey = '...'; // hex encoded private key seed
      const processId = '...'; // hex encoded process ID
      const address = '...'; // hex encoded Ethereum address

      const cspRoot = global.DavinciCrypto.cspCensusRoot(censusOrigin, privKey);
      console.log('Calculated CSP Census Root:', cspRoot.data);
      const proofResult = DavinciCrypto.cspSign(censusOrigin, privKey, processId, address);
      console.log('Generated CSP Proof:', proofResult.data);

      const verifyResult = DavinciCrypto.cspVerify(JSON.stringify(proof));
      console.log('Proof verified:', verifyResult);
   });
</script>
```


## 📚 Additional Resources

### Documentation
- **API Documentation:** [api/README.md](https://github.com/vocdoni/davinci-node/tree/main/api)
- **Technical Whitepaper:** [whitepaper.vocdoni.io](https://whitepaper.vocdoni.io)
- **Protocol Website:** [davinci.vote](https://davinci.vote)

### Development
- **Source Code:** [github.com/vocdoni/davinci-node](https://github.com/vocdoni/davinci-node)
- **Issues & Bug Reports:** [GitHub Issues](https://github.com/vocdoni/davinci-node/issues)

### Community
- **Vocdoni Website:** [vocdoni.io](https://vocdoni.io)
- **Discord:** [Join our community](https://chat.vocdoni.io) 

---

**Built with ❤️ by the Vocdoni team**
