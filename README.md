# DAVINCI Node

Davinci-Node is the main implementation of the https://DAVINCI.vote protocol. A  zkSNARK-based voting sequencer that processes encrypted ballots and generates cryptographic proofs for decentralized voting. It supports distributed proving through a worker system that allows multiple nodes to collaborate in processing zkSNARK proofs.

## Configuration Flags

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

Davinci Node operates in two modes:

1. **Master Mode**: A complete sequencer that processes votes, manages the ballot queue, and can optionally distribute zkSNARK proving workload to worker nodes.
2. **Worker Mode**: A lightweight node that only handles zkSNARK proof generation for ballots assigned by a master node.


The worker system enables distributed zkSNARK proving, allowing the computational workload to be distributed across multiple nodes. This architecture provides:

Workers authenticate using a UUID-based system:
- Master generates a UUID from a configurable seed using `hash(UrlSeed)`
- Workers must know the correct master URL including the UUID
- This provides a simple authentication layer for the worker network

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

To run a worker node:

```bash
davinci-sequencer \
  --worker.masterURL="http://master-host:9090/workers/<UUID>" \
  --worker.address="0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"
```

**Key Worker Flags:**
- `--worker.masterURL` or `-w`: Full URL to master's worker endpoint (required for worker mode)
- `--worker.address` or `-a`: Ethereum address identifying this worker

**Note:** The worker master URL (including the secret UUID) can be fetch from the logs of the master node. Search for a message like this:

`INF > worker API enabled url=/workers/8d969eef-6eca-d3c2-9a3a-629280e686cf`
