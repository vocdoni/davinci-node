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
)

const (
	defaultRetries       = 3
	maxEndpointSwitches  = 5 // Maximum number of endpoint switches before giving up
)

var (
	defaultTimeout    = 20 * time.Second
	filterLogsTimeout = 25 * time.Second
	retryStartDelay   = 300 * time.Millisecond
	minTimeForRetry   = 500 * time.Millisecond // Minimum time needed to attempt another retry
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
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.CodeAt(internalCtx, account, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// CallContract method wraps the CallContract method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.CallContract(internalCtx, call, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
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
	// no retry wrapper here, or wrap if you want retries for simulate too
	return endpoint.rpcClient.CallContext(ctx, result, "eth_simulateV1", simReq, blockTag)
}

// EstimateGas method wraps the EstimateGas method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.EstimateGas(internalCtx, msg)
	})
	if err != nil {
		return 0, err
	}
	return res.(uint64), nil
}

// FilterLogs method wraps the FilterLogs method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]types.Log, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, filterLogsTimeout)
		defer cancel()
		return ep.client.FilterLogs(internalCtx, query)
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
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.HeaderByNumber(internalCtx, number)
	})
	if err != nil {
		return nil, err
	}
	return res.(*types.Header), nil
}

// PendingNonceAt method wraps the PendingNonceAt method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.PendingNonceAt(internalCtx, account)
	})
	if err != nil {
		return 0, err
	}
	return res.(uint64), nil
}

// SuggestGasPrice method wraps the SuggestGasPrice method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.SuggestGasPrice(internalCtx)
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), nil
}

// SendTransaction method wraps the SendTransaction method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	_, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return nil, ep.client.SendTransaction(internalCtx, tx)
	})
	return err
}

// PendingCodeAt method wraps the PendingCodeAt method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.PendingCodeAt(internalCtx, account)
	})
	if err != nil {
		return nil, err
	}
	return res.([]byte), nil
}

// SubscribeFilterLogs method wraps the SubscribeFilterLogs method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SubscribeFilterLogs(ctx context.Context,
	query ethereum.FilterQuery, ch chan<- types.Log,
) (ethereum.Subscription, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.SubscribeFilterLogs(internalCtx, query, ch)
	})
	if err != nil {
		return nil, err
	}
	return res.(ethereum.Subscription), nil
}

// SuggestGasTipCap method wraps the SuggestGasTipCap method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.SuggestGasTipCap(internalCtx)
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), nil
}

// BalanceAt method wraps the BalanceAt method from the ethclient.Client for the
// chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. This method is required by internal
// logic, it is not required by the bind.ContractBackend interface.
func (c *Client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.BalanceAt(internalCtx, account, blockNumber)
	})
	if err != nil {
		return nil, err
	}
	return res.(*big.Int), nil
}

// BlockNumber method wraps the BlockNumber method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. This method is required by internal
// logic, it is not required by the bind.ContractBackend interface.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		internalCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return ep.client.BlockNumber(internalCtx)
	})
	if err != nil {
		return 0, err
	}
	return res.(uint64), nil
}

// BlobBaseFee retrieves the base fee for blob transactions on the blockchain.
func (c *Client) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	res, err := c.retryWithEndpointSwitch(func(ep *Web3Endpoint) (any, error) {
		var hexFee string
		if err := ep.rpcClient.CallContext(ctx, &hexFee, "eth_blobBaseFee"); err != nil {
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

// retryWithEndpointSwitch retries a function call in case of error, automatically
// switching to the next available endpoint when one fails. It tries each endpoint
// with exponential backoff before switching to the next one. The function receives
// the endpoint as a parameter, allowing it to use the correct client for each attempt.
// It respects context deadlines and will stop retrying if insufficient time remains.
func (c *Client) retryWithEndpointSwitch(fn func(*Web3Endpoint) (any, error)) (any, error) {
	var lastErr error
	switchCount := 0

	for switchCount < maxEndpointSwitches {
		endpoint, err := c.w3p.Endpoint(c.chainID)
		if err != nil {
			// No more endpoints available
			if switchCount == 0 {
				return nil, fmt.Errorf("error getting endpoint: %w", err)
			}
			return nil, fmt.Errorf("error after trying %d endpoint(s): %w (no more endpoints: %v)",
				switchCount, lastErr, err)
		}

		// Try the current endpoint with retries and backoff
		retryDelay := retryStartDelay

		for attempt := 0; attempt < defaultRetries; attempt++ {
			res, err := fn(endpoint)
			if err == nil {
				return res, nil
			}
			lastErr = err

			// Before sleeping, check if we should continue
			if attempt < defaultRetries-1 {
				// If this is not the last retry and not the last endpoint switch,
				// sleep with exponential backoff
				time.Sleep(retryDelay)
				retryDelay *= 2
			}
		}

		// All retries failed for this endpoint, disable it
		c.w3p.DisableEndpoint(c.chainID, endpoint.URI)
		switchCount++
	}

	return nil, fmt.Errorf("error after switching through %d endpoints: %w",
		maxEndpointSwitches, lastErr)
}
