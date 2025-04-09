package chainlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/vocdoni/vocdoni-z-sandbox/log"
)

const (
	// jsonRPCVersion is the standard JSON-RPC version used in requests and responses
	jsonRPCVersion = "2.0"

	requiredEthLogsBlocks = 50001 // Number of blocks to check for eth_getLogs
)

var (
	// randShuffle allows tests to override the default shuffle implementation
	randShuffle = rand.Shuffle
	// ChainListURL is the URL to get the chain metadata from an external source.
	ChainListURL = "https://chainlist.org/rpcs.json"
	// defaultTimeout is the default timeout for RPC health checks
	defaultTimeout = 3 * time.Second
)

var (
	// chainListInfo ensures that the chain data is fetched only once
	chainListInfo sync.Once
	// chainMutex protects concurrent access to the chain maps
	chainMutex sync.RWMutex
	// chainsByID stores chains indexed by their chain ID
	chainsByID map[uint64]*Chain
	// chainsByShortName stores chains indexed by their short name
	chainsByShortName map[string]*Chain
	// initErr stores any error that occurred during initialization
	initErr error
)

// Chain represents blockchain network metadata from chainlist.org
type Chain struct {
	Name           string         `json:"name"`
	Chain          string         `json:"chain"`
	Icon           string         `json:"icon,omitempty"`
	RPC            []RPCEntry     `json:"rpc"`
	Features       []Feature      `json:"features,omitempty"`
	Faucets        []string       `json:"faucets"`
	NativeCurrency NativeCurrency `json:"nativeCurrency"`
	InfoURL        string         `json:"infoURL"`
	ShortName      string         `json:"shortName"`
	ChainID        uint64         `json:"chainId"`
	NetworkID      int            `json:"networkId"`
	Slip44         int            `json:"slip44,omitempty"`
	ENS            *ENSConfig     `json:"ens,omitempty"`
	Explorers      []Explorer     `json:"explorers,omitempty"`
	TVL            float64        `json:"tvl,omitempty"`
	ChainSlug      string         `json:"chainSlug"`
}

// RPCEntry represents an RPC endpoint for a blockchain
type RPCEntry struct {
	URL          string `json:"url"`
	Tracking     string `json:"tracking,omitempty"`
	IsOpenSource bool   `json:"isOpenSource,omitempty"`
}

// Feature represents a blockchain feature
type Feature struct {
	Name string `json:"name"`
}

// NativeCurrency represents the blockchain's native currency
type NativeCurrency struct {
	Name     string `json:"name"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}

// ENSConfig represents Ethereum Name Service configuration
type ENSConfig struct {
	Registry string `json:"registry"`
}

// Explorer represents a blockchain explorer
type Explorer struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Icon     string `json:"icon,omitempty"`
	Standard string `json:"standard"`
}

// jsonRPCRequest represents a JSON-RPC 2.0 request
type jsonRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response
type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	ID int `json:"id"`
}

// ChainInfo represents a simplified chain information with ID and short name
type ChainInfo struct {
	ChainID   uint64
	ShortName string
}

// initialize fetches the chain list data and populates the maps
func initialize() error {
	resp, err := http.Get(ChainListURL)
	if err != nil {
		return fmt.Errorf("failed to fetch chain list: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch chain list: %s", resp.Status)
	}

	var chains []Chain
	if err := json.NewDecoder(resp.Body).Decode(&chains); err != nil {
		return fmt.Errorf("failed to decode chain list: %w", err)
	}

	// Initialize the maps
	chainsByID = make(map[uint64]*Chain, len(chains))
	chainsByShortName = make(map[string]*Chain, len(chains))

	// Populate the maps
	for i := range chains {
		chain := &chains[i]
		chainsByID[chain.ChainID] = chain
		chainsByShortName[chain.ShortName] = chain
	}

	return nil
}

// healthCheckFunc is the function type for checking endpoint health
type healthCheckFunc func(ctx context.Context, endpoint string, timeout time.Duration, chainID uint64) bool

// isHealthyEndpoint checks if an RPC endpoint responds correctly to both eth_blockNumber and eth_getLogs requests
var isHealthyEndpoint healthCheckFunc = func(bctx context.Context, endpoint string, timeout time.Duration, chainID uint64) bool {
	// Set up client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Step 1: Check eth_blockNumber
	blockNumReq := jsonRPCRequest{
		JSONRPC: jsonRPCVersion,
		Method:  "eth_blockNumber",
		Params:  []any{},
		ID:      1,
	}

	blockNumReqBody, err := json.Marshal(blockNumReq)
	if err != nil {
		return false
	}

	// Create a context with timeout for the request
	ctx, cancel := context.WithTimeout(bctx, time.Second*5)
	defer cancel()

	// Create the HTTP request
	blockNumHttpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(blockNumReqBody))
	if err != nil {
		return false
	}
	blockNumHttpReq.Header.Set("Content-Type", "application/json")

	blockNumResp, err := client.Do(blockNumHttpReq)
	if err != nil {
		return false
	}
	defer func() {
		if err := blockNumResp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	// Check status code
	if blockNumResp.StatusCode != http.StatusOK {
		return false
	}

	// Parse the response
	var blockNumRpcResp jsonRPCResponse
	if err := json.NewDecoder(blockNumResp.Body).Decode(&blockNumRpcResp); err != nil {
		return false
	}

	// Check for valid response (JSONRPC 2.0, no error, has result)
	if blockNumRpcResp.JSONRPC != jsonRPCVersion || blockNumRpcResp.Error != nil || blockNumRpcResp.Result == nil {
		return false
	}

	// Check if the block number is greater than 0x0
	blockNumberStr, ok := blockNumRpcResp.Result.(string)
	if !ok {
		return false
	}

	// Block number should start with "0x" and be greater than "0x0"
	if len(blockNumberStr) <= 2 || blockNumberStr == "0x0" {
		return false
	}

	// Step 2: Check eth_getLogs support
	// Calculate fromBlock as blockNumber - requiredEthLogsBlocks
	var fromBlock string
	// Parse blockNumber hex string to integer
	var blockNumber uint64
	if len(blockNumberStr) > 2 && blockNumberStr[:2] == "0x" {
		_, err = fmt.Sscanf(blockNumberStr[2:], "%x", &blockNumber)
		if err != nil {
			return false
		}
	} else {
		return false // Invalid block number format
	}

	// Calculate fromBlock
	if blockNumber > 5001 {
		fromBlock = fmt.Sprintf("0x%x", blockNumber-requiredEthLogsBlocks)
	} else {
		// If blockNumber <= requiredEthLogsBlocks, use 0x0 as fromBlock
		fromBlock = "0x0"
	}

	// Create eth_getLogs request
	getLogsParams := map[string]interface{}{
		"fromBlock": fromBlock,
		"toBlock":   "latest",
		"address":   "0x1d0b39c0239329955b9F0E8791dF9Aa84133c861", // Dummy address
	}

	getLogsReq := jsonRPCRequest{
		JSONRPC: jsonRPCVersion,
		Method:  "eth_getLogs",
		Params:  []any{getLogsParams},
		ID:      1,
	}

	getLogsReqBody, err := json.Marshal(getLogsReq)
	if err != nil {
		return false
	}

	getLogsHttpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(getLogsReqBody))
	if err != nil {
		return false
	}
	getLogsHttpReq.Header.Set("Content-Type", "application/json")

	getLogsResp, err := client.Do(getLogsHttpReq)
	if err != nil {
		return false
	}
	defer func() {
		if err := getLogsResp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	// Check status code
	if getLogsResp.StatusCode != http.StatusOK {
		return false
	}

	// Parse the response
	var getLogsRpcResp jsonRPCResponse
	if err := json.NewDecoder(getLogsResp.Body).Decode(&getLogsRpcResp); err != nil {
		return false
	}

	// Check for valid response (JSONRPC 2.0, no error)
	// Note: the result can be an empty array, which is valid
	if getLogsRpcResp.JSONRPC != jsonRPCVersion || getLogsRpcResp.Error != nil {
		return false
	}

	// Step 3: Check eth_chainId
	chainIdReq := jsonRPCRequest{
		JSONRPC: jsonRPCVersion,
		Method:  "eth_chainId",
		Params:  []any{},
		ID:      1,
	}

	chainIdReqBody, err := json.Marshal(chainIdReq)
	if err != nil {
		return false
	}

	chainIdHttpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(chainIdReqBody))
	if err != nil {
		return false
	}
	chainIdHttpReq.Header.Set("Content-Type", "application/json")

	chainIdResp, err := client.Do(chainIdHttpReq)
	if err != nil {
		return false
	}
	defer func() {
		if err := chainIdResp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	// Check status code
	if chainIdResp.StatusCode != http.StatusOK {
		return false
	}

	// Parse the response
	var chainIdRpcResp jsonRPCResponse
	if err := json.NewDecoder(chainIdResp.Body).Decode(&chainIdRpcResp); err != nil {
		return false
	}

	// Check for valid response (JSONRPC 2.0, no error, has result)
	if chainIdRpcResp.JSONRPC != jsonRPCVersion || chainIdRpcResp.Error != nil || chainIdRpcResp.Result == nil {
		return false
	}

	// Check if chain ID matches expected value
	chainIdHex, ok := chainIdRpcResp.Result.(string)
	if !ok {
		return false
	}

	// Convert hex string to uint64
	// Remove "0x" prefix if present
	if len(chainIdHex) > 2 && chainIdHex[:2] == "0x" {
		chainIdHex = chainIdHex[2:]
	}

	// Parse the hex value
	var endpointChainId uint64
	_, err = fmt.Sscanf(chainIdHex, "%x", &endpointChainId)
	if err != nil {
		return false
	}

	// Verify chain ID matches expected value
	return endpointChainId == chainID
}

// EndpointList returns a randomly ordered slice of HTTP endpoints for a chain,
// identified either by chain short name. It returns at most numEndpoints entries.
// If there are not enough endpoints available, it returns all available endpoints.
// Only healthy endpoints that respond correctly to an eth_blockNumber request are returned.
func EndpointList(chainName string, numEndpoints int) ([]string, error) {
	// Ensure we've fetched the data
	chainListInfo.Do(func() {
		initErr = initialize()
	})

	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize chain list: %w", initErr)
	}

	chainMutex.RLock()
	defer chainMutex.RUnlock()
	// Check if the chain exists
	var chain *Chain
	var ok bool
	chain, ok = chainsByShortName[chainName]
	if !ok {
		return nil, fmt.Errorf("chain with short name %q not found", chainName)
	}

	// Extract HTTP URLs
	urls := make([]string, 0, len(chain.RPC))
	for _, rpc := range chain.RPC {
		urls = append(urls, rpc.URL)
	}

	// Shuffle the URLs
	randShuffle(len(urls), func(i, j int) {
		urls[i], urls[j] = urls[j], urls[i]
	})

	// Create context for health checks with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up channels for concurrent processing
	type result struct {
		url     string
		healthy bool
	}
	resultChan := make(chan result)

	// Start a goroutine for each URL to check health
	var wg sync.WaitGroup
	for _, url := range urls {
		wg.Add(1)
		go func(endpoint string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				// Early cancellation if we already have enough endpoints
				return
			default:
				healthy := isHealthyEndpoint(ctx, endpoint, defaultTimeout, chain.ChainID)
				log.Debugw("RPC health check", "url", endpoint, "healthy", healthy)
				resultChan <- result{url: endpoint, healthy: healthy}
			}
		}(url)
	}

	// Close the result channel when all checks are done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect healthy endpoints
	healthyURLs := make([]string, 0, numEndpoints)
	for r := range resultChan {
		if r.healthy {
			healthyURLs = append(healthyURLs, r.url)

			// If we have enough endpoints, cancel remaining checks
			if len(healthyURLs) >= numEndpoints && numEndpoints > 0 {
				cancel() // Signal other goroutines to stop
				break
			}
		}
	}

	return healthyURLs, nil
}

// ChainList returns a map of chain short names to their respective chain IDs.
// This allows users to discover available chains and their identifiers.
func ChainList() (map[string]uint64, error) {
	// Ensure we've fetched the data
	chainListInfo.Do(func() {
		initErr = initialize()
	})

	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize chain list: %w", initErr)
	}

	chainMutex.RLock()
	defer chainMutex.RUnlock()

	result := make(map[string]uint64, len(chainsByShortName))
	for shortName, chain := range chainsByShortName {
		result[shortName] = chain.ChainID
	}

	return result, nil
}
