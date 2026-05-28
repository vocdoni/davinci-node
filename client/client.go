package client

import (
	"context"
	"fmt"
	"time"

	"github.com/vocdoni/davinci-node/api"
	"github.com/vocdoni/davinci-node/circuits"
	"github.com/vocdoni/davinci-node/client/census3"
	"github.com/vocdoni/davinci-node/client/httpclient"
	"github.com/vocdoni/davinci-node/spec/params"
	"github.com/vocdoni/davinci-node/types"
	"github.com/vocdoni/davinci-node/web3"
)

// Client holds the services required to interact with a Davinci services and
// contracts.
type Client struct {
	ctx           context.Context
	config        *ClientConfig
	httpClient    *httpclient.HTTPclient
	census3Client *census3.Census3Client
	runtimes      *web3.RuntimeRouter
	sequencerInfo api.SequencerInfo
	ballotproof   *circuits.CircuitArtifacts
}

// NewClient creates a new Client instance with the context and the
// configuration provided.
func NewClient(ctx context.Context, config *ClientConfig) (*Client, error) {
	// Check config provided
	if !config.Valid() {
		return nil, fmt.Errorf("invalid config provided")
	}
	// Create inner context
	cli := &Client{
		ctx:    ctx,
		config: config,
	}
	// Init sequencer client
	if err := cli.initSequencer(); err != nil {
		return nil, err
	}
	// Init census3 client
	if err := cli.initCensus3(); err != nil {
		return nil, err
	}
	// Init web3 clients
	if err := cli.initWeb3(); err != nil {
		return nil, err
	}
	// Init ballotproof circuit artifacts
	if err := cli.initBallotProofCircuit(); err != nil {
		return nil, err
	}
	return cli, nil
}

// initWeb3 method initializes the web3 multinetwork runtimes for the current
// client. If no RPCs are provided, the default config will be used based on
// the available sequencer networks.
func (c *Client) initWeb3() error {
	// If no RPCs are provided, fetch default config for the sequencer chain IDs
	if len(c.config.Web3.RPCs) == 0 {
		var chainIDs []uint
		for _, network := range c.sequencerInfo.Networks {
			chainIDs = append(chainIDs, uint(network.ChainID))
		}
		var err error
		c.config.Web3, err = web3.DefautlWeb3Config(chainIDs, c.config.Web3.PrivKey)
		if err != nil {
			return err
		}
	}

	runtimes, err := c.config.Web3.InitRuntimes(c.ctx, false)
	if err != nil {
		return fmt.Errorf("failed to initialize runtimes: %w", err)
	}
	c.runtimes, err = web3.NewRuntimeRouter(runtimes...)
	if err != nil {
		return fmt.Errorf("failed to create runtime router: %w", err)
	}
	return nil
}

// initSequencer method initializes the HTTP client to interact with the
// sequencer API.
func (c *Client) initSequencer() error {
	// Create a API client
	var err error
	c.httpClient, err = httpclient.NewHTTPClient(c.config.SequencerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create sequencer client: %w", err)
	}
	// Wait for the sequencer to be ready, make ping request until it responds
	pingCtx, cancel := context.WithTimeout(c.ctx, 2*time.Minute)
	defer cancel()
	for isConnected := false; !isConnected; {
		select {
		case <-pingCtx.Done():
			return fmt.Errorf("timeout reached while connecting to sequencer")
		default:
			if err := c.httpClient.Ping(api.PingEndpoint); err == nil {
				isConnected = true
				break
			}
			time.Sleep(10 * time.Second)
		}
	}
	// Fetch and store the sequencer info
	if c.sequencerInfo, err = c.SequencerInfo(); err != nil {
		return fmt.Errorf("failed to fetch the sequencer information: %w", err)
	}
	return nil
}

// initCensus3 method initializes the census3 client.
func (c *Client) initCensus3() error {
	var err error
	c.census3Client, err = census3.NewClient(c.ctx, c.config.Census3Endpoint)
	if err != nil {
		return fmt.Errorf("failed to create census3 client: %w", err)
	}
	return nil
}

// initBallotProofCircuit method initializes the ballot proof circuit artifacts
// based on the sequencer info to be used by the client.
func (c *Client) initBallotProofCircuit() error {
	// Create circuit artifacts based on the sequencer info
	var err error
	circuitWasmArtifact := &circuits.Artifact{
		RemoteURL: c.sequencerInfo.CircuitURL,
	}
	circuitWasmArtifact.Hash, err = types.HexStringToHexBytes(c.sequencerInfo.CircuitHash)
	if err != nil {
		return fmt.Errorf("the sequencer info contains an invalid circuit hash: %w", err)
	}
	provingKeyArtifact := &circuits.Artifact{
		RemoteURL: c.sequencerInfo.ProvingKeyURL,
	}
	provingKeyArtifact.Hash, err = types.HexStringToHexBytes(c.sequencerInfo.ProvingKeyHash)
	if err != nil {
		return fmt.Errorf("the sequencer info contains an invalid circuit hash: %w", err)
	}
	// Initialize the circuit artifacts and download them
	c.ballotproof = circuits.NewCircomCircuitArtifacts("ballotproof", params.BallotProofCurve, circuitWasmArtifact, provingKeyArtifact, nil)
	return c.ballotproof.Download(c.ctx)
}
