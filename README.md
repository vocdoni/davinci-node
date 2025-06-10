# DAVINCI Node

{**D**ecentralized **A**utonomous **V**ote **I**ntegrity **N**etwork with **C**ryptographic **I**nference}


Davinci-Node is the main implementation of the https://davinci.vote protocol. A  zkSNARK-based voting network that processes encrypted ballots and generates cryptographic proofs for decentralized voting. Read the full technical Whitepaper at https://whitepaper.vocdoni.io

## Command line

In a Go ready environment, run:  `go run ./cmd/davinci-sequencer -h`


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


## Worker Mode

Davinci-Node supports distributed proving through a worker system that allows multiple nodes to collaborate in processing zkSNARK proofs. It can operate in two modes:

1. **Master Mode**: A complete sequencer that processes votes, manages the ballot queue, and can optionally distribute zkSNARK proving workload to worker nodes.
2. **Worker Mode**: A lightweight node that only handles zkSNARK proof generation for ballots assigned by a master node.

The worker system enables distributed zkSNARK proving, allowing the computational workload to be distributed across multiple nodes.

Workers authenticate using a UUID-based system:
- Master generates a UUID from a configurable seed using `hash(UrlSeed)`
- Workers must know the correct master URL including the UUID
- Workers are expected to provide an Ethereum address so the Master node keeps track of the success/failed jobs for each worker (enables potential payouts)

### Configuration

To run a master node with worker support enabled:

```bash
davinci-sequencer \
  --web3.privkey="0x..." \
  --api.workerSeed="my-secret-seed" \
  --worker.timeout=1m
```

**Key Master Flags:**
- `--api.workerSeed`: Enable worker endpoints with authentication seed (required for worker support)
- `--worker.timeout`: Maximum time a worker can hold a job before timeout (default: 1m)
- `--api.host`: API host address (default: 0.0.0.0)
- `--api.port`: API port (default: 9090)

The worker master URL (including the secret UUID) can be fetch from the logs. Search for a message like this:

> INF [...] > worker API enabled url=/workers/8d969eef-6eca-d3c2-9a3a-629280e686cf

To run a worker node:

```bash
davinci-sequencer \
  --worker.masterURL="http://master-host:9090/workers/<UUID>" \
  --worker.address="0x1111122222333334444455555666667777788888"
```

**Key Worker Flags:**
- `--worker.masterURL` or `-w`: Full URL to master's worker endpoint (required for worker mode)
- `--worker.address` or `-a`: Ethereum address identifying this worker


# Run with docker

Copy the example ENV file: `cp .env.example .env`

If runing as a **Worker node**, only two variables need to be configured:

```bash
DAVINCI_WORKER_MASTERURL="http://master-host:9090/workers/<UUID>"
DAVINCI_WORKER_ADDRESS="0x1111122222333334444455555666667777788888"
```

And start the container `docker compose build; docker compose up -d --force-recreate`

---

If runing as a **Master node**, the following variables need to be set:

```bash
DAVINCI_WEB3_PRIVKEY=<hex private key with funds> # currently Sepolia ETH
DAVINCI_WEB3_NETWORK=sep # for Sepolia
DAVINCI_API_WORKERSEED=someRandomSeed # just provide some entropy to generate the UUID
```

The node exposes a HTTP/REST API, see the documentation at https://github.com/vocdoni/davinci-node/tree/main/api

```json
$ curl -s http://localhost:9090/info | jq .
{
  "circuitUrl": "https://circuits.ams3.cdn.digitaloceanspaces.com/dev/ballot_proof.wasm",
  "circuitHash": "5a6f7d40c1e74c238cc282c4bcc22a0a623b6fa8426c01cd7e8ef45e34394faf",
  "ballotProofWasmHelperUrl": "https://github.com/vocdoni/davinci-node/raw/refs/heads/main/cmd/ballotproof-wasm/ballotproof.wasm",
  "ballotProofWasmHelperHash": "78e66e787ca075445da0009ff203cfb9acf18f759c787cbf2e3eade99e72fd61",
  "ballotProofWasmHelperExecJsUrl": "https://github.com/vocdoni/davinci-node/raw/refs/heads/main/cmd/ballotproof-wasm/wasm_exec.js",
  "ballotProofWasmHelperExecJsHash": "0c949f4996f9a89698e4b5c586de32249c3b69b7baadb64d220073cc04acba14",
  "provingKeyUrl": "https://circuits.ams3.cdn.digitaloceanspaces.com/dev/ballot_proof_pkey.zkey",
  "provingKeyHash": "f4bc379bb933946a558bdbe504e93037c8049fbb809fb515e452f0f370e27cef",
  "verificationKeyUrl": "https://circuits.ams3.cdn.digitaloceanspaces.com/dev/ballot_proof_vkey.json",
  "verificationKeyHash": "833c8f97ed01858e083f3c8b04965f168400a2cc205554876e49d32b14ddebe8",
  "contracts": {
    "process": "0x7c2Fdd6b411e40d9f02B496D1cA1EA767bC3d337",
    "organization": "0x82A6492db3c26E666634FF8EFDac3Fe8dbe5652C",
  }
}
```

## Web UI

The sequencer includes a web UI dashboard accessible at `http://localhost:9090/app`

The UI provides:
- Smart contract addresses with block explorer links
- Process list with statistics and real-time updates
- Detailed process information including voting results
- Filtering and sorting capabilities
- API URL configuration

### Configuration

The Web UI supports multiple configuration methods:

1. **Environment Variables**: Set `SEQUENCER_API_URL` and `BLOCK_EXPLORER_URL` when running the container
2. **In-App**: Use the input field at the top of the dashboard to change the API URL on the fly
