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
func mockHealthyEndpoint(_ context.Context, _ string, _ time.Duration) bool {
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

	// Test getting endpoints by chainID
	t.Run("Get by chainID", func(t *testing.T) {
		endpoints, err := EndpointList(1, "", 10)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		if len(endpoints) != 3 {
			t.Errorf("Expected 3 endpoints, got %d", len(endpoints))
		}

		// Verify all endpoints are present (in any order due to randomization)
		expectURLs := map[string]bool{
			"https://eth-rpc-1.example.com": true,
			"https://eth-rpc-2.example.com": true,
			"https://eth-rpc-3.example.com": true,
		}

		for _, url := range endpoints {
			if !expectURLs[url] {
				t.Errorf("Unexpected endpoint URL: %s", url)
			}
			// Mark as found
			expectURLs[url] = false
		}
	})

	// Test getting endpoints by shortName
	t.Run("Get by shortName", func(t *testing.T) {
		endpoints, err := EndpointList(0, "arb1", 10)
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
		endpoints, err := EndpointList(1, "", 2)
		if err != nil {
			t.Fatalf("EndpointList failed: %v", err)
		}

		if len(endpoints) != 2 {
			t.Errorf("Expected 2 endpoints, got %d", len(endpoints))
		}
	})

	// Test error cases
	t.Run("Error cases", func(t *testing.T) {
		// Test non-existent chainID
		_, err := EndpointList(999, "", 10)
		if err == nil {
			t.Error("Expected error for non-existent chainID, got nil")
		}

		// Test non-existent shortName
		_, err = EndpointList(0, "nonexistent", 10)
		if err == nil {
			t.Error("Expected error for non-existent shortName, got nil")
		}

		// Test neither chainID nor shortName provided
		_, err = EndpointList(0, "", 10)
		if err == nil {
			t.Error("Expected error when neither chainID nor shortName provided, got nil")
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
func mockSelectiveHealthCheck(healthyURLs map[string]bool) healthCheckFunc {
	return func(_ context.Context, endpoint string, _ time.Duration) bool {
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
		endpoints, err := EndpointList(1, "", 10)
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
		endpoints, err := EndpointList(1, "", 2)
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

// resetGlobals resets the package global variables for testing
func resetGlobals() {
	chainListInfo = sync.Once{}
	chainsByID = nil
	chainsByShortName = nil
	initErr = nil
}
