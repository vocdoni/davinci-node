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
type healthCheckFunc func(ctx context.Context, endpoint string, timeout time.Duration) bool

// isHealthyEndpoint checks if an RPC endpoint responds correctly to a basic eth_blockNumber request
var isHealthyEndpoint healthCheckFunc = func(ctx context.Context, endpoint string, timeout time.Duration) bool {
	// Create JSON-RPC request payload
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_blockNumber",
		Params:  []any{},
		ID:      1,
	}

	// Marshal the request to JSON
	reqBody, err := json.Marshal(req)
	if err != nil {
		return false
	}

	// Create HTTP request with context
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return false
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Set up client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Make the request
	resp, err := client.Do(httpReq)
	if err != nil {
		return false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("failed to close response body: %v", err)
		}
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return false
	}

	// Parse the response
	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return false
	}

	// Check for valid response (JSONRPC 2.0, no error, has result)
	return rpcResp.JSONRPC == "2.0" && rpcResp.Error == nil && rpcResp.Result != nil
}

// EndpointList returns a randomly ordered slice of HTTP endpoints for a chain,
// identified either by chainID or shortName. If chainID is non-zero, it looks up
// by chainID, otherwise it uses the shortName. It returns at most numEndpoints entries.
// If there are not enough endpoints available, it returns all available endpoints.
// Only healthy endpoints that respond correctly to an eth_blockNumber request are returned.
func EndpointList(chainID uint64, shortName string, numEndpoints int) ([]string, error) {
	// Ensure we've fetched the data
	chainListInfo.Do(func() {
		initErr = initialize()
	})

	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize chain list: %w", initErr)
	}

	chainMutex.RLock()
	var chain *Chain
	if chainID != 0 {
		var ok bool
		chain, ok = chainsByID[chainID]
		if !ok {
			chainMutex.RUnlock()
			return nil, fmt.Errorf("chain ID %d not found", chainID)
		}
	} else if shortName != "" {
		var ok bool
		chain, ok = chainsByShortName[shortName]
		if !ok {
			chainMutex.RUnlock()
			return nil, fmt.Errorf("chain with short name %q not found", shortName)
		}
	} else {
		chainMutex.RUnlock()
		return nil, fmt.Errorf("either chainID or shortName must be provided")
	}

	// Extract HTTP URLs
	urls := make([]string, 0, len(chain.RPC))
	for _, rpc := range chain.RPC {
		urls = append(urls, rpc.URL)
	}
	chainMutex.RUnlock()

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
				healthy := isHealthyEndpoint(ctx, endpoint, defaultTimeout)
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
