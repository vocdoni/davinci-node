package chainlist

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

// testRand is a package-level random number generator for consistent test results
var testRand = rand.New(rand.NewSource(1234))

// Override the default random shuffle for tests
func init() {
	// Replace the global rand.Shuffle with our deterministic version for tests
	randShuffle = func(n int, swap func(i, j int)) {
		testRand.Shuffle(n, swap)
	}
}

// mockHealthyEndpoint is a replacement for isHealthyEndpoint that always returns true for testing
// This mock simulates an endpoint that supports eth_blockNumber, eth_getLogs, and has matching chainID
func mockHealthyEndpoint(_ context.Context, _ string, _ time.Duration, _ uint64) bool {
	return true
}

// mockChainList sets up a mock server for testing chain data and mocks the health check
func mockChainList(t *testing.T, mockChains []Chain) (cleanup func()) {
	// Save the original healthcheck function and replace it with our mock
	originalHealthCheck := isHealthyEndpoint
	isHealthyEndpoint = mockHealthyEndpoint

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if we're accessing the expected endpoint
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		// Return mock chain data for chainlist
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mockChains); err != nil {
			t.Fatalf("Failed to encode mock chains: %v", err)
		}
	}))

	// Save the original URL
	originalURL := ChainListURL
	// Set the mock server URL
	ChainListURL = server.URL

	return func() {
		// Restore the original URL and close the server
		ChainListURL = originalURL
		// Restore the original health check function
		isHealthyEndpoint = originalHealthCheck
		server.Close()
		// Reset global state for next test
		resetGlobals()
	}
}

// TestEndpointList tests the EndpointList function with various inputs
func TestEndpointList(t *testing.T) {
	// Create mock chains for testing
	mockChains := []Chain{
		{
			Name:      "Ethereum Mainnet",
			Chain:     "ETH",
			ShortName: "eth",
			ChainID:   1,
			RPC: []RPCEntry{
				{URL: "https://eth-rpc-1.example.com"},
				{URL: "https://eth-rpc-2.example.com"},
				{URL: "https://eth-rpc-3.example.com"},
			},
		},
		{
			Name:      "Arbitrum One",
			Chain:     "ARB1",
			ShortName: "arb1",
			ChainID:   42161,
			RPC: []RPCEntry{
				{URL: "https://arb-rpc-1.example.com"},
				{URL: "https://arb-rpc-2.example.com"},
			},
		},
	}

	// Setup mock server and cleanup
	cleanup := mockChainList(t, mockChains)
	defer cleanup()

	// Test getting endpoints by shortName
	t.Run("Get by shortName", func(t *testing.T) {
		endpoints, err := EndpointList("arb1", 10)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		if len(endpoints) != 2 {
			t.Errorf("Expected 2 endpoints, got %d", len(endpoints))
		}

		// Verify all endpoints are present (in any order due to randomization)
		expectURLs := map[string]bool{
			"https://arb-rpc-1.example.com": true,
			"https://arb-rpc-2.example.com": true,
		}

		for _, url := range endpoints {
			if !expectURLs[url] {
				t.Errorf("Unexpected endpoint URL: %s", url)
			}
			// Mark as found
			expectURLs[url] = false
		}
	})

	// Test limiting the number of endpoints
	t.Run("Limit number of endpoints", func(t *testing.T) {
		endpoints, err := EndpointList("eth", 2)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		if len(endpoints) != 2 {
			t.Errorf("Expected 2 endpoints, got %d", len(endpoints))
		}
	})
}

// TestGetChainList tests the GetChainList function
func TestGetChainList(t *testing.T) {
	// Create mock chains for testing
	mockChains := []Chain{
		{
			Name:      "Ethereum Mainnet",
			Chain:     "ETH",
			ShortName: "eth",
			ChainID:   1,
		},
		{
			Name:      "Arbitrum One",
			Chain:     "ARB1",
			ShortName: "arb1",
			ChainID:   42161,
		},
	}

	// Setup mock server and cleanup
	cleanup := mockChainList(t, mockChains)
	defer cleanup()

	// Test getting the chain list
	t.Run("Get chain list", func(t *testing.T) {
		chainList, err := ChainList()
		if err != nil {
			t.Fatalf("GetChainList failed: %v", err)
		}

		expectedList := map[string]uint64{
			"eth":  1,
			"arb1": 42161,
		}

		if !reflect.DeepEqual(chainList, expectedList) {
			t.Errorf("Expected chain list %v, got %v", expectedList, chainList)
		}
	})
}

// mockSelectiveHealthCheck creates a health check function that only returns healthy for specific URLs
// This mock simulates endpoints that may or may not support both eth_blockNumber and eth_getLogs
func mockSelectiveHealthCheck(healthyURLs map[string]bool) healthCheckFunc {
	return func(_ context.Context, endpoint string, _ time.Duration, _ uint64) bool {
		return healthyURLs[endpoint]
	}
}

// TestHealthyEndpointFiltering tests that only healthy endpoints are returned
func TestHealthyEndpointFiltering(t *testing.T) {
	// Create mock chains for testing
	mockChains := []Chain{
		{
			Name:      "Test Chain",
			Chain:     "TEST",
			ShortName: "test",
			ChainID:   1,
			RPC: []RPCEntry{
				{URL: "https://healthy-1.example.com"},
				{URL: "https://healthy-2.example.com"},
				{URL: "https://unhealthy-1.example.com"},
				{URL: "https://unhealthy-2.example.com"},
				{URL: "https://healthy-3.example.com"},
			},
		},
	}

	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mockChains); err != nil {
			t.Fatalf("Failed to encode mock chains: %v", err)
		}
	}))
	defer server.Close()

	// Save original values
	originalURL := ChainListURL
	originalHealthCheck := isHealthyEndpoint
	defer func() {
		// Restore original values
		ChainListURL = originalURL
		isHealthyEndpoint = originalHealthCheck
		resetGlobals()
	}()

	// Set up test values
	ChainListURL = server.URL
	healthyURLs := map[string]bool{
		"https://healthy-1.example.com":   true,
		"https://healthy-2.example.com":   true,
		"https://healthy-3.example.com":   true,
		"https://unhealthy-1.example.com": false,
		"https://unhealthy-2.example.com": false,
	}
	isHealthyEndpoint = mockSelectiveHealthCheck(healthyURLs)

	// Reset global state
	resetGlobals()

	// Test that only healthy endpoints are returned
	t.Run("Only healthy endpoints returned", func(t *testing.T) {
		endpoints, err := EndpointList("test", 10)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		if len(endpoints) != 3 {
			t.Errorf("Expected 3 healthy endpoints, got %d", len(endpoints))
		}

		// Check that all returned endpoints are healthy
		for _, url := range endpoints {
			if !healthyURLs[url] {
				t.Errorf("Got unhealthy endpoint: %s", url)
			}
		}
	})

	// Test that limiting works with healthy filtering
	t.Run("Limit healthy endpoints", func(t *testing.T) {
		endpoints, err := EndpointList("test", 2)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		if len(endpoints) != 2 {
			t.Errorf("Expected 2 healthy endpoints, got %d", len(endpoints))
		}

		// Check that all returned endpoints are healthy
		for _, url := range endpoints {
			if !healthyURLs[url] {
				t.Errorf("Got unhealthy endpoint: %s", url)
			}
		}
	})
}

// TestEnhancedHealthCheck tests the enhanced endpoint health check functionality
func TestEnhancedHealthCheck(t *testing.T) {
	// Create mock chains for testing
	mockChains := []Chain{
		{
			Name:      "Test Chain",
			Chain:     "TEST",
			ShortName: "test",
			ChainID:   1,
			RPC: []RPCEntry{
				{URL: "https://valid-block-and-logs.example.com"},  // Both valid
				{URL: "https://valid-block-no-logs.example.com"},   // Only valid block but no getLogs
				{URL: "https://zero-block-valid-logs.example.com"}, // Zero block but valid getLogs
				{URL: "https://zero-block-no-logs.example.com"},    // Neither valid
			},
		},
	}

	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mockChains); err != nil {
			t.Fatalf("Failed to encode mock chains: %v", err)
		}
	}))
	defer server.Close()

	// Save original values
	originalURL := ChainListURL
	originalHealthCheck := isHealthyEndpoint
	defer func() {
		// Restore original values
		ChainListURL = originalURL
		isHealthyEndpoint = originalHealthCheck
		resetGlobals()
	}()

	// Set up test values
	ChainListURL = server.URL

	t.Run("Only endpoints with valid block number and getLogs support", func(t *testing.T) {
		// Create a map to simulate different endpoint behaviors
		endpointBehaviors := map[string]struct {
			blockNumberValid bool
			supportsGetLogs  bool
			correctChainID   bool
		}{
			"https://valid-block-and-logs.example.com":  {true, true, true},
			"https://valid-block-no-logs.example.com":   {true, false, true},
			"https://zero-block-valid-logs.example.com": {false, true, true},
			"https://zero-block-no-logs.example.com":    {false, false, true},
		}

		// Create a custom health check that tests all three conditions
		isHealthyEndpoint = func(_ context.Context, endpoint string, _ time.Duration, chainID uint64) bool {
			behavior, exists := endpointBehaviors[endpoint]
			if !exists {
				return false
			}
			// Endpoint must satisfy all three conditions: valid block, getLogs support, and correct chainID
			return behavior.blockNumberValid && behavior.supportsGetLogs && behavior.correctChainID
		}

		// Reset global state
		resetGlobals()

		endpoints, err := EndpointList("test", 10)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		// Only one endpoint should be returned (the one that satisfies both conditions)
		if len(endpoints) != 1 {
			t.Errorf("Expected 1 healthy endpoint, got %d", len(endpoints))
		}

		// Verify it's the correct endpoint
		if len(endpoints) > 0 && endpoints[0] != "https://valid-block-and-logs.example.com" {
			t.Errorf("Expected healthy endpoint 'https://valid-block-and-logs.example.com', got %s", endpoints[0])
		}
	})
}

// resetGlobals resets the package global variables for testing
func resetGlobals() {
	chainListInfo = sync.Once{}
	chainsByID = nil
	chainsByShortName = nil
	initErr = nil
}
