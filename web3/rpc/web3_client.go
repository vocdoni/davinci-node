package rpc

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/vocdoni/davinci-node/log"
)

const (
	// defaultRetries is the number of times to retry an RPC call on the same endpoint before switching
	defaultRetries = 2
	// defaultRetrySleep is the time to wait between retries on the same endpoint
	defaultRetrySleep = 200 * time.Millisecond
)

var (
	defaultTimeout    = 3 * time.Second
	filterLogsTimeout = 5 * time.Second
)

// Client struct implements bind.ContractBackend interface for a web3 pool with
// an specific chainID. It allows to interact with the blockchain using the
// methods provided by the interface balancing the load between the available
// endpoints in the pool for the chainID.
type Client struct {
	w3p     *Web3Pool
	chainID uint64
}

// EthClient method returns the ethclient.Client for the chainID of the Client
// instance. It returns an error if the chainID is not found in the pool.
func (c *Client) EthClient() (*ethclient.Client, error) {
	endpoint, err := c.w3p.Endpoint(c.chainID)
	if err != nil {
		return nil, fmt.Errorf("error getting endpoint for chainID %d: %w", c.chainID, err)
	}
	return endpoint.client, nil
}

// RPCClient method returns the rpc.Client for the chainID of the Client
// instance. It returns an error if the chainID is not found in the pool.
func (c *Client) RPCClient() (*rpc.Client, error) {
	endpoint, err := c.w3p.Endpoint(c.chainID)
	if err != nil {
		return nil, fmt.Errorf("error getting endpoint for chainID %d: %w", c.chainID, err)
	}
	return endpoint.rpcClient, nil
}

// CodeAt method wraps the CodeAt method from the ethclient.Client for the
// chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.CodeAt(internalCtx, account, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), err
}

// CallContract method wraps the CallContract method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.CallContract(internalCtx, call, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), err
}

func (c *Client) CallSimulation(
	ctx context.Context,
	result interface{},
	simReq interface{},
	blockTag string,
) error {
	endpoint, err := c.w3p.Endpoint(c.chainID)
	if err != nil {
		return fmt.Errorf("error getting endpoint for chainID %d: %w", c.chainID, err)
	}
	return endpoint.rpcClient.CallContext(ctx, result, "eth_simulateV1", simReq, blockTag)
}

// EstimateGas method wraps the EstimateGas method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.EstimateGas(internalCtx, msg)
	})
	if err != nil {
		return 0, err
	}
	return res.(uint64), err
}

// FilterLogs method wraps the FilterLogs method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, filterLogsTimeout)
		defer cancel()
		return endpoint.client.FilterLogs(internalCtx, query)
	})
	if err != nil {
		return nil, err
	}
	return res.([]types.Log), nil
}

// HeaderByNumber method wraps the HeaderByNumber method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.HeaderByNumber(internalCtx, number)
	})
	if err != nil {
		return nil, err
	}
	return res.(*types.Header), err
}

// PendingNonceAt method wraps the PendingNonceAt method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.PendingNonceAt(internalCtx, account)
	})
	if err != nil {
		return 0, err
	}
	return res.(uint64), err
}

// SuggestGasPrice method wraps the SuggestGasPrice method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.SuggestGasPrice(internalCtx)
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), err
}

// SendTransaction method wraps the SendTransaction method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	_, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return nil, endpoint.client.SendTransaction(internalCtx, tx)
	})
	return err
}

// PendingCodeAt method wraps the PendingCodeAt method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.PendingCodeAt(internalCtx, account)
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), err
}

// SubscribeFilterLogs method wraps the SubscribeFilterLogs method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SubscribeFilterLogs(ctx context.Context,
	query ethereum.FilterQuery, ch chan<- types.Log,
) (ethereum.Subscription, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.SubscribeFilterLogs(internalCtx, query, ch)
	})
	if err != nil {
		return nil, err
	}
	return res.(ethereum.Subscription), err
}

// SuggestGasTipCap method wraps the SuggestGasTipCap method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.SuggestGasTipCap(internalCtx)
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), err
}

// BalanceAt method wraps the BalanceAt method from the ethclient.Client for the
// chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. This method is required by internal
// logic, it is not required by the bind.ContractBackend interface.
func (c *Client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.BalanceAt(internalCtx, account, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), err
}

// BlockNumber method wraps the BlockNumber method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. This method is required by internal
// logic, it is not required by the bind.ContractBackend interface.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return endpoint.client.BlockNumber(internalCtx)
	})
	if err != nil {
		return 0, err
	}
	return res.(uint64), err
}

// BlobBaseFee retrieves the base fee for blob transactions on the blockchain.
func (c *Client) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	res, err := c.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		var hexFee string
		if err := endpoint.rpcClient.CallContext(ctx, &hexFee, "eth_blobBaseFee"); err != nil {
			return nil, err
		}
		f, ok := new(big.Int).SetString(strings.TrimPrefix(hexFee, "0x"), 16)
		if !ok {
			return nil, fmt.Errorf("invalid hex fee %q", hexFee)
		}
		return f, nil
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), nil
}

// retryAndCheckErr method retries a function call with endpoint switching.
// The function fn receives a fresh endpoint on each attempt. It first retries
// on the current endpoint, and if that fails, it disables the endpoint and tries
// the next available one. This continues until either the operation succeeds or
// all endpoints have been exhausted. This ensures no RPC calls are lost due to
// a single endpoint failure. Thread-safe: all operations are mutex-protected.
func (c *Client) retryAndCheckErr(fn func(*Web3Endpoint) (any, error)) (any, error) {
	// Track which endpoints we've tried to avoid infinite loops
	triedEndpoints := make(map[string]bool)

	// Get total number of available endpoints for this chainID
	totalEndpoints := c.w3p.NumberOfEndpoints(c.chainID, false)
	if totalEndpoints == 0 {
		return nil, fmt.Errorf("no endpoints available for chainID %d", c.chainID)
	}

	var lastErr error
	endpointAttempts := 0

	// Try all available endpoints
	for endpointAttempts < totalEndpoints {
		// Get current endpoint
		endpoint, err := c.w3p.Endpoint(c.chainID)
		if err != nil {
			return nil, fmt.Errorf("error getting endpoint for chainID %d: %w", c.chainID, err)
		}

		// Check if we've already tried this endpoint
		if triedEndpoints[endpoint.URI] {
			log.Errorw(lastErr, fmt.Sprintf("endpoint rotation returned already-tried endpoint %s for chainID %d",
				endpoint.URI, c.chainID))
			return nil, fmt.Errorf("endpoint rotation failed for chainID %d: %w", c.chainID, lastErr)
		}
		triedEndpoints[endpoint.URI] = true

		// Retry on current endpoint
		var res any
		for retry := 0; retry < defaultRetries; retry++ {
			res, err = fn(endpoint)
			if err == nil {
				// Success! Log if we had to switch endpoints
				if endpointAttempts > 0 {
					log.Infow("RPC call succeeded after endpoint switch",
						"chainID", c.chainID,
						"successfulURI", endpoint.URI,
						"endpointAttempts", endpointAttempts+1,
						"retriesOnEndpoint", retry+1)
				}
				return res, nil
			}
			lastErr = err
			if retry < defaultRetries-1 {
				time.Sleep(defaultRetrySleep)
			}
		}

		// All retries failed on this endpoint, disable it and try next
		log.Warnw("endpoint failed after retries, switching to next",
			"chainID", c.chainID,
			"failedURI", endpoint.URI,
			"error", err,
			"retries", defaultRetries,
			"endpointAttempt", endpointAttempts+1)

		c.w3p.DisableEndpoint(c.chainID, endpoint.URI)
		endpointAttempts++
	}

	// All endpoints exhausted
	log.Errorw(lastErr, fmt.Sprintf("no more endpoints available after failures for chainID %d, tried %d endpoints",
		c.chainID, len(triedEndpoints)))
	return nil, fmt.Errorf("all endpoints exhausted for chainID %d after %d attempts: %w",
		c.chainID, endpointAttempts, lastErr)
}
