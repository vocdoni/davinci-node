package rpc

// This package contains the Web3Pool struct, which is a pool of Web3Endpoint
// instances. It allows to add, remove and get endpoints, as well as to get the
// chainID by short name. It also provides an implementation of the
// bind.ContractBackend interface for a web3 pool with an specific chainID.
// It allows to interact with the blockchain using the methods provided by the
// interface balancing the load between the available endpoints in the pool for
// every chainID.
// The pool balances the load between the available endpoints in the pool for
// every chainID, allowing to use the endpoints concurrently and switch between
// them flagging them as available if they fail to keep the pool healthy. If
// every endpoint fails for a chainID, the pool resets the available flag for
// all the endpoints and starts again.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/vocdoni/davinci-node/log"
)

const (
	// DefaultMaxWeb3ClientRetries is the default number of retries to connectEth to
	// a web3 provider.
	DefaultMaxWeb3ClientRetries = 5
	// shortNameSourceUri is the URI to get the chain metadata from an external
	shortNameSourceUri = "https://chainid.network/chains_mini.json"
	// checkWeb3EndpointsTimeout is the timeout to check the web3 endpoints.
	checkWeb3EndpointsTimeout = time.Second * 10
	// foundTxErrMessage is the error message when a transaction is found but it
	// is not supported.
	foundTxErrMessage = "transaction type not supported"
)

var dumpRPCBodies = os.Getenv("DAVINCI_LOG_DUMPRPC") == "true"

// notFoundTxRgx is a regular expression to match the error message when a
// transaction is not found.
var notFoundTxRgx = regexp.MustCompile(`not\s[be\s|]*found`)

// debuggableTransport is a custom http.RoundTripper that can log RPC requests and responses.
type debuggableTransport struct {
	rt http.RoundTripper
}

// RoundTrip implements the http.RoundTripper interface for debuggableTransport.
// If dumpRPCBodies is true, logs the request and response bodies, useful for debugging RPC issues.
func (d debuggableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if dumpRPCBodies && req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		log.Logger().Debug().CallerSkipFrame(100).Msgf(">>> %s %s\n%s", req.Method, req.URL, b)
		req.Body = io.NopCloser(bytes.NewReader(b))
	}

	resp, err := d.rt.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if dumpRPCBodies && resp.Body != nil {
		b, _ := io.ReadAll(resp.Body)
		log.Logger().Debug().CallerSkipFrame(100).Msgf("<<< %s %s: %s\n%s", req.Method, req.URL, resp.Status, b)
		resp.Body = io.NopCloser(bytes.NewReader(b))
	}
	return resp, nil
}

// Web3Pool struct contains a map of chainID-[]*Web3Endpoint, where
// the key is the chainID and the value is a list of Web3Endpoint. It also
// contains a list of all the Web3Endpoint metadata. It provides methods to
// add, remove and get endpoints, as well as to get the chainID by short name.
// It allows to support multiple endpoints for the same chainID and switch
// between them looking for the available one.
type Web3Pool struct {
	endpoints map[uint64]*Web3Iterator
	metadata  []*Web3Endpoint
}

// NewWeb3Pool method returns a new *Web3Pool instance, initialized
// with the endpoints list available in https://chainid.network.
// It returns an error if the metadata cannot be retrieved or decoded.
func NewAutomaticWeb3Pool() (*Web3Pool, error) {
	// get chains information from external source
	res, err := http.Get(shortNameSourceUri)
	if err != nil {
		return nil, fmt.Errorf("error getting chains information from external source: %v", err)
	}
	chainsData := []*Web3Endpoint{}
	if err := json.NewDecoder(res.Body).Decode(&chainsData); err != nil {
		return nil, fmt.Errorf("error decoding chains information from external source: %v", err)
	}
	return &Web3Pool{
		endpoints: make(map[uint64]*Web3Iterator),
		metadata:  chainsData,
	}, nil
}

// NewWeb3Pool method returns a new *Web3Pool instance.
func NewWeb3Pool() *Web3Pool {
	return &Web3Pool{
		endpoints: make(map[uint64]*Web3Iterator),
	}
}

// AddEndpoint method adds a new web3 provider URI to the Web3Pool.
// It returns the chainID of the endpoint added to the pool.
func (nm *Web3Pool) AddEndpoint(uri string) (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkWeb3EndpointsTimeout)
	defer cancel()
	// init the web3 client
	client, err := connectNodeEthereumAPI(ctx, uri)
	if err != nil {
		return 0, fmt.Errorf("error dialing web3 provider uri '%s': %w", uri, err)
	}

	// get the chainID from the web3 endpoint
	bChainID, err := client.ChainID(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting the chainID from the web3 provider '%s': %w", uri, err)
	}
	chainID := bChainID.Uint64()

	// check if the endpoint is an archive node or not
	isArchive, err := isArchiveNode(ctx, client)
	if err != nil {
		log.Debugw("error checking if the web3 provider is an archive node", "chainID", chainID, "error", err)
	}
	// add the endpoint to the pool
	endpoint := &Web3Endpoint{
		ChainID:   chainID,
		URI:       uri,
		client:    client,
		IsArchive: isArchive,
	}
	if _, ok := nm.endpoints[chainID]; !ok {
		nm.endpoints[chainID] = NewWeb3Iterator(endpoint)
	} else {
		nm.endpoints[chainID].Add(endpoint)
	}
	return chainID, nil
}

// DelEndpoint method removes a web3 provider URI from the *Web3Pool
// instance. It closes the client and removes the endpoint from the list of
// endpoints for the chainID where it was found.
func (nm *Web3Pool) DelEndoint(uri string) {
	for _, endpoints := range nm.endpoints {
		endpoints.Disable(uri)
	}
}

// Endpoint method returns the Web3Endpoint configured for the chainID
// provided. It returns the first available endpoint. If no available endpoint
// is found, returns an error.
func (nm *Web3Pool) Endpoint(chainID uint64) (*Web3Endpoint, error) {
	if endpoints, ok := nm.endpoints[chainID]; ok {
		return endpoints.Next()
	}
	return nil, fmt.Errorf("no endpoint found for chainID %d", chainID)
}

// DisableEndpoint method sets the available flag to false for the URI provided
// in the chainID provided.
func (nm *Web3Pool) DisableEndpoint(chainID uint64, uri string) {
	if endpoints, ok := nm.endpoints[chainID]; ok {
		availableBefore := endpoints.Available()
		endpoints.Disable(uri)
		availableAfter := endpoints.Available()

		// Log the disable operation
		if availableBefore != availableAfter {
			log.Warnw("endpoint disabled",
				"chainID", chainID,
				"uri", uri,
				"availableEndpoints", availableAfter,
				"disabledEndpoints", endpoints.Disabled())

			// If all endpoints were disabled and got reset, log that too
			if availableAfter > availableBefore {
				log.Infow("all endpoints were disabled, reset to available",
					"chainID", chainID,
					"resetEndpoints", availableAfter)
			}
		}
	}
}

// NumberOfEndpoints method returns the total number (or just the available ones)
// of endpoints for the chainID provided.
func (nm *Web3Pool) NumberOfEndpoints(chainID uint64, onlyAvailable bool) int {
	if endpoints, ok := nm.endpoints[chainID]; ok {
		n := endpoints.Available()
		if !onlyAvailable {
			n += endpoints.Disabled()
		}
		return n
	}
	return 0
}

// Client method returns a new *Client instance for the chainID provided.
// It returns an error if the endpoint is not found.
func (nm *Web3Pool) Client(chainID uint64) (*Client, error) {
	if _, err := nm.Endpoint(chainID); err != nil {
		return nil, fmt.Errorf("error getting endpoint for chainID %d: %w", chainID, err)
	}
	return &Client{w3p: nm, chainID: chainID}, nil
}

// CurrentBlockNumbers method returns a map of uint64-uint64, where the key is
// the chainID and the value is the current block number of the network.
func (nm *Web3Pool) CurrentBlockNumbers(ctx context.Context) (map[uint64]uint64, error) {
	blockNumbers := make(map[uint64]uint64)
	for chainID := range nm.endpoints {
		cli, err := nm.Endpoint(chainID)
		if err != nil {
			return nil, fmt.Errorf("error getting endpoint for chainID %d: %w", chainID, err)
		}
		blockNumber, err := cli.client.BlockNumber(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting block number for chainID %d: %w", chainID, err)
		}
		blockNumbers[chainID] = blockNumber
	}
	return blockNumbers, nil
}

// SupportedNetworks method returns a list of all the supported Web3Endpoint
// metadata. It returns the chainID, name and shortName of unique supported
// chains.
func (nm *Web3Pool) SupportedNetworks() []*Web3Endpoint {
	var supported []*Web3Endpoint
	for chainID := range nm.endpoints {
		for _, data := range nm.metadata {
			if data.ChainID == chainID {
				supported = append(supported, &Web3Endpoint{
					ChainID: chainID,
				})
				break
			}
		}
	}
	return supported
}

// NetworkInfoByChainID method returns the Web3Endpoint metadata for the
// chainID provided. It returns nil if the chainID is not found.
func (nm *Web3Pool) NetworkInfoByChainID(chainID uint64) *Web3Endpoint {
	for _, data := range nm.metadata {
		if data.ChainID == chainID {
			return data
		}
	}
	return nil
}

// connectNodeEthereumAPI method returns a new *ethclient.Client instance for the URI provided.
// It retries to connect to the web3 provider if it fails, up to the
// DefaultMaxWeb3ClientRetries times.
func connectNodeEthereumAPI(ctx context.Context, uri string) (client *ethclient.Client, err error) {
	httpClient := &http.Client{
		Transport: debuggableTransport{rt: http.DefaultTransport},
	}
	for range DefaultMaxWeb3ClientRetries {
		rpcClient, err := gethrpc.DialOptions(ctx, uri, gethrpc.WithHTTPClient(httpClient))
		if err != nil {
			continue
		}
		return ethclient.NewClient(rpcClient), nil
	}
	return nil, fmt.Errorf("error dialing web3 provider uri '%s': %w", uri, err)
}

// isArchiveNode method returns true if the web3 client is an archive node. To
// determine if the client is an archive node, checks the transactions of the
// block 1 of the chain. If client finds transactions, it is an archive node. If
// it does not find transactions, it is not an archive node. If an error occurs,
// it returns false and the error.
func isArchiveNode(ctx context.Context, client *ethclient.Client) (bool, error) {
	block, err := client.BlockByNumber(ctx, big.NewInt(1))
	if err != nil {
		if strings.Contains(err.Error(), foundTxErrMessage) {
			return true, nil
		}
		return false, fmt.Errorf("error getting block 1: %w", err)
	}

	if _, err := client.TransactionCount(ctx, block.Hash()); err != nil {
		if notFoundTxRgx.MatchString(err.Error()) {
			return false, nil
		}
		if strings.Contains(err.Error(), foundTxErrMessage) {
			return true, nil
		}
		return false, fmt.Errorf("error getting transaction in block 1: %w", err)
	}
	return true, nil
}
