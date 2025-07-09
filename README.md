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
   DAVINCI_WEB3_NETWORK=sep # for Sepolia
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
| `--web3.network` | `-n` | `sep` | Network to use (sep, mainnet, etc.) |
| `--web3.rpc` | `-r` | | Custom RPC endpoints (comma-separated) |
| `--api.host` | `-h` | `0.0.0.0` | API host address |
| `--api.port` | `-p` | `9090` | API port number |
| `--api.workerSeed` | none | | URL seed for worker authentication |
| `--batch.time` | `-b` | `5m` | Batch processing time window |
| `--log.level` | `-l` | `info` | Log level (debug, info, warn, error) |
| `--log.output` | `-o` | `stdout` | Log output destination |
| `--datadir` | `-d` | `~/.davinci` | Data directory path |
| `--worker.masterURL` | `-w` | | Master URL for worker mode |
| `--worker.address` | `-a` | | Worker Ethereum address |
| `--worker.timeout` | none | `1m` | Worker job timeout duration |

## ⚡ Run a Worker Node

Worker nodes are lightweight components that handle zkSNARK proof generation for ballots assigned by a master sequencer node. This enables distributed proving and helps scale the network.

### Setup Steps

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
   DAVINCI_WORKER_MASTERURL="http://master-host:9090/workers/<UUID>"
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
   docker compose build
   ```

3. **Start the worker again:**
   ```bash
   docker compose up -d --force-recreate sequencer
   ```

### Configuration Notes

> ⚠️ **Important:** The Master URL (including the UUID) must be provided by the owner of the Master Sequencer node. See the [Workers API section](#enable-workers-api) for details on how to obtain this URL.

> 💡 **Note:** The Ethereum address can be any valid address. It's used for accounting purposes and tracking success/failed jobs, but does not need to own any funds.

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
