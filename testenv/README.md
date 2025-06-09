# Test Environment Setup

This directory contains a Docker Compose setup for testing the Davinci Node with smart contracts deployed on a local Anvil network.

## Components

- **Anvil**: Local Ethereum network for testing
- **Deployer**: Deploys smart contracts and serves contract addresses via HTTP
- **Sequencer**: Davinci Node sequencer service with automatically configured contract addresses

## Usage

1. Start the test environment:
   ```bash
   cd testenv
   docker-compose up
   ```

2. The services will start in the following order:
   - Anvil starts first
   - Deployer waits for Anvil, then deploys contracts and serves addresses.json
   - Sequencer waits for deployer to be healthy, fetches contract addresses, and starts

3. Access the services:
   - Anvil RPC: http://localhost:8545
   - Contract addresses (JSON): http://localhost:8000/addresses.json
   - Contract addresses (ENV): http://localhost:8000/addresses.env
   - Sequencer API: http://localhost:9090

4. Stop the environment:
   ```bash
   docker-compose down -v
   ```

## Available Accounts for testing

- address: `0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266`
- privatekey: `0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80`
- mnemonic: `test test test test test test test test test test test junk`
- derivation path: `m/44'/60'/0'/0/`
