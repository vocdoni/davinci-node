package web3

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
)

// WaitReadyRPC waits for the RPC endpoint to be ready by checking if it returns a valid block number.
// It will continuously try to get the block number until the context is canceled or a valid block number is received.
// The function returns an error if the context is canceled or if another error occurs.
func WaitReadyRPC(ctx context.Context, rpcURL string) error {
	log.Debugw("waiting for RPC to be ready", "url", rpcURL)
	// Connect directly to the Ethereum client
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to RPC endpoint: %w", err)
	}
	defer client.Close()

	// Poll until the RPC returns a valid block number or context is canceled
	retryInterval := 500 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled while waiting for RPC to be ready: %w", ctx.Err())
		default:
			// Check if RPC is ready by getting the block number
			blockNumber, err := client.BlockNumber(ctx)
			if err == nil && blockNumber > 0 {
				// RPC is ready with a non-zero block number
				log.Infow("RPC is ready", "url", rpcURL, "blockNumber", blockNumber)
				return nil
			}

			// Wait before retrying
			time.Sleep(retryInterval)
		}
	}
}
