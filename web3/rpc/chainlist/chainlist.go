package chainlist

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
)

var (
	// ChainListURL is the URL to get the chain metadata from an external source.
	ChainListURL = "https://chainlist.org/rpcs.json"
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
	defer resp.Body.Close()

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

// EndpointList returns a randomly ordered slice of HTTP endpoints for a chain,
// identified either by chainID or shortName. If chainID is non-zero, it looks up
// by chainID, otherwise it uses the shortName. It returns at most numEndpoints entries.
// If there are not enough endpoints available, it returns all available endpoints.
func EndpointList(chainID uint64, shortName string, numEndpoints int) ([]string, error) {
	// Ensure we've fetched the data
	chainListInfo.Do(func() {
		initErr = initialize()
	})

	if initErr != nil {
		return nil, fmt.Errorf("failed to initialize chain list: %w", initErr)
	}

	chainMutex.RLock()
	defer chainMutex.RUnlock()

	var chain *Chain
	if chainID != 0 {
		var ok bool
		chain, ok = chainsByID[chainID]
		if !ok {
			return nil, fmt.Errorf("chain ID %d not found", chainID)
		}
	} else if shortName != "" {
		var ok bool
		chain, ok = chainsByShortName[shortName]
		if !ok {
			return nil, fmt.Errorf("chain with short name %q not found", shortName)
		}
	} else {
		return nil, fmt.Errorf("either chainID or shortName must be provided")
	}

	// Extract HTTP URLs
	urls := make([]string, 0, len(chain.RPC))
	for _, rpc := range chain.RPC {
		urls = append(urls, rpc.URL)
	}

	// Shuffle the URLs
	rand.Shuffle(len(urls), func(i, j int) {
		urls[i], urls[j] = urls[j], urls[i]
	})

	// Return at most numEndpoints
	if numEndpoints > 0 && numEndpoints < len(urls) {
		urls = urls[:numEndpoints]
	}

	return urls, nil
}

// GetChainList returns a map of chain short names to their respective chain IDs.
// This allows users to discover available chains and their identifiers.
func GetChainList() (map[string]uint64, error) {
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
