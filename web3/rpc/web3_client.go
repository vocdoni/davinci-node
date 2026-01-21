package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
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

// permanentErrorPatterns defines error patterns that indicate permanent
// failures that should not be retried. These are typically contract-level
// rejections that will never succeed regardless of gas price or retries.
// Add new patterns here as they are discovered and confirmed.
var permanentErrorPatterns = []string{
	"execution reverted", // Contract rejected the transaction
}

// IsPermanentError checks if an error represents a permanent failure that
// should not be retried.
func IsPermanentError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	for _, pattern := range permanentErrorPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

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

// CodeAt method wraps the CodeAt method from the ethclient.Client for the
// chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) ([]byte, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.CodeAt(ctxWithTimeout, account, blockNumber)
	})
}

// CallContract method wraps the CallContract method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) ([]byte, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.CallContract(ctxWithTimeout, call, blockNumber)
	})
}

func (c *Client) CallSimulation(ctx context.Context, result any, simReq any, blockTag string) error {
	_, err := retrySwitchingEndpoints(c, func(client *ethclient.Client) (struct{}, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return struct{}{}, client.Client().CallContext(ctxWithTimeout, result, "eth_simulateV1", simReq, blockTag)
	})
	return err
}

// EstimateGas method wraps the EstimateGas method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (uint64, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.EstimateGas(ctxWithTimeout, msg)
	})
}

// FilterLogs method wraps the FilterLogs method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. Required by the bind.ContractBackend
// interface.
func (c *Client) FilterLogs(ctx context.Context, query ethereum.FilterQuery) ([]gethtypes.Log, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) ([]gethtypes.Log, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, filterLogsTimeout)
		defer cancel()
		return client.FilterLogs(ctxWithTimeout, query)
	})
}

// HeaderByNumber method wraps the HeaderByNumber method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) HeaderByNumber(ctx context.Context, number *big.Int) (*gethtypes.Header, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*gethtypes.Header, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.HeaderByNumber(ctxWithTimeout, number)
	})
}

// HeaderByHash method wraps the HeaderByHash method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails.
func (c *Client) HeaderByHash(ctx context.Context, blockHash common.Hash) (*gethtypes.Header, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*gethtypes.Header, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.HeaderByHash(ctxWithTimeout, blockHash)
	})
}

// TransactionByHash method wraps the TransactionByHash method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails.
func (c *Client) TransactionByHash(ctx context.Context, txHash common.Hash) (*gethtypes.Transaction, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*gethtypes.Transaction, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		tx, _, err := client.TransactionByHash(ctxWithTimeout, txHash)
		// we have no way to return the isPending bool, so we ignore it
		return tx, err
	})
}

// TransactionReceipt method wraps the TransactionReceipt method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails.
func (c *Client) TransactionReceipt(ctx context.Context, txHash common.Hash) (*gethtypes.Receipt, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*gethtypes.Receipt, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.TransactionReceipt(ctxWithTimeout, txHash)
	})
}

// PendingNonceAt method wraps the PendingNonceAt method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) PendingNonceAt(ctx context.Context, account common.Address) (uint64, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (uint64, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.PendingNonceAt(ctxWithTimeout, account)
	})
}

// SuggestGasPrice method wraps the SuggestGasPrice method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*big.Int, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.SuggestGasPrice(ctxWithTimeout)
	})
}

// SendTransaction method wraps the SendTransaction method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) SendTransaction(ctx context.Context, tx *gethtypes.Transaction) error {
	_, err := retrySwitchingEndpoints(c, func(client *ethclient.Client) (struct{}, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return struct{}{}, client.SendTransaction(ctxWithTimeout, tx)
	})
	return err
}

// PendingCodeAt method wraps the PendingCodeAt method from the ethclient.Client
// for the chainID of the Client instance. It returns an error if the chainID is
// not found in the pool or if the method fails. Required by the
// bind.ContractBackend interface.
func (c *Client) PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) ([]byte, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.PendingCodeAt(ctxWithTimeout, account)
	})
}

// SubscribeFilterLogs method wraps the SubscribeFilterLogs method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SubscribeFilterLogs(ctx context.Context,
	query ethereum.FilterQuery, ch chan<- gethtypes.Log,
) (ethereum.Subscription, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (ethereum.Subscription, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.SubscribeFilterLogs(ctxWithTimeout, query, ch)
	})
}

// SuggestGasTipCap method wraps the SuggestGasTipCap method from the
// ethclient.Client for the chainID of the Client instance. It returns an error
// if the chainID is not found in the pool or if the method fails. Required by
// the bind.ContractBackend interface.
func (c *Client) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*big.Int, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.SuggestGasTipCap(ctxWithTimeout)
	})
}

// BalanceAt method wraps the BalanceAt method from the ethclient.Client for the
// chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. This method is required by internal
// logic, it is not required by the bind.ContractBackend interface.
func (c *Client) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*big.Int, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.BalanceAt(ctxWithTimeout, account, blockNumber)
	})
}

// BlockNumber method wraps the BlockNumber method from the ethclient.Client for
// the chainID of the Client instance. It returns an error if the chainID is not
// found in the pool or if the method fails. This method is required by internal
// logic, it is not required by the bind.ContractBackend interface.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (uint64, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.BlockNumber(ctxWithTimeout)
	})
}

// BlobBaseFee retrieves the base fee for blob transactions on the blockchain.
func (c *Client) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	return retrySwitchingEndpoints(c, func(client *ethclient.Client) (*big.Int, error) {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
		return client.BlobBaseFee(ctxWithTimeout)
	})
}

// retrySwitchingEndpoints method retries a function call with endpoint switching.
// The function fn receives a fresh endpoint on each attempt. It first retries
// on the current endpoint, and if that fails, it disables the endpoint and tries
// the next available one. This continues until either the operation succeeds or
// all endpoints have been exhausted. This ensures no RPC calls are lost due to
// a single endpoint failure. Thread-safe: all operations are mutex-protected.
func retrySwitchingEndpoints[T any](c *Client, fn func(*ethclient.Client) (T, error)) (T, error) {
	var zero T
	// Track which endpoints we've tried to avoid infinite loops
	triedEndpoints := make(map[string]bool)

	// Get total number of available endpoints for this chainID
	totalEndpoints := c.w3p.NumberOfEndpoints(c.chainID, false)
	if totalEndpoints == 0 {
		return zero, fmt.Errorf("no endpoints available for chainID %d", c.chainID)
	}

	var lastErr error
	endpointAttempts := 0

	// Try all available endpoints
	for endpointAttempts < totalEndpoints {
		// Get current endpoint
		endpoint, err := c.w3p.Endpoint(c.chainID)
		if err != nil {
			return zero, fmt.Errorf("error getting endpoint for chainID %d: %w", c.chainID, err)
		}

		// Check if we've already tried this endpoint
		if triedEndpoints[endpoint.URI] {
			log.Errorw(lastErr, fmt.Sprintf("endpoint rotation returned already-tried endpoint %s for chainID %d",
				endpoint.URI, c.chainID))
			return zero, fmt.Errorf("endpoint rotation failed for chainID %d: %w", c.chainID, lastErr)
		}
		triedEndpoints[endpoint.URI] = true

		// Retry on current endpoint
		var res T
		for retry := range defaultRetries {
			res, err = fn(endpoint.client)
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
			if rpcErr := ParseError(err); rpcErr != nil {
				lastErr = fmt.Errorf("%w (code: %d, data: %s)", err, rpcErr.Code, rpcErr.Data)
			} else {
				lastErr = err
			}
			if IsPermanentError(err) {
				log.Warnw("RPC returned permanent error, not retrying",
					"error", lastErr,
					"chainID", c.chainID,
					"failedURI", endpoint.URI,
					"endpointAttempts", endpointAttempts+1,
					"retriesOnEndpoint", retry+1)
				return zero, fmt.Errorf("RPC call failed with permanent error, not retrying: %w", err)
			}
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
	return zero, fmt.Errorf("all endpoints exhausted for chainID %d after %d attempts: %w",
		c.chainID, endpointAttempts, lastErr)
}

// RPCError is the error returned by the RPC server
type RPCError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    hexutil.Bytes `json:"data"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s (code: %d, data: %s)", e.Message, e.Code, e.Data.String())
}

func (e *RPCError) ErrorCode() int {
	return e.Code
}

func (e *RPCError) ErrorData() any {
	return e.Data
}

// ParseError tries to extract Data and Code from error,
// to reconstruct a *RPCError.
func ParseError(err error) *RPCError {
	if err == nil {
		return nil
	}
	if e, ok := err.(*RPCError); ok {
		return e
	}

	out := &RPCError{Message: err.Error()}

	// Code (if available)
	var rpcErr gethrpc.Error
	if errors.As(err, &rpcErr) {
		out.Code = rpcErr.ErrorCode()
		out.Message = rpcErr.Error()
	}

	// Data (if available)
	var dataErr gethrpc.DataError
	if errors.As(err, &dataErr) {
		switch v := dataErr.ErrorData().(type) {
		case []byte:
			out.Data = hexutil.Bytes(v)
		case string:
			if b, derr := hexutil.Decode(v); derr == nil {
				out.Data = hexutil.Bytes(b)
			}
		}
	}

	return out
}
