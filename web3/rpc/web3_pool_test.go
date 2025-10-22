package rpc

import (
	"errors"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"
)

// TestEndpointSwitchingOnFailure tests that when an endpoint fails,
// the retry mechanism switches to the next available endpoint
func TestEndpointSwitchingOnFailure(t *testing.T) {
	c := qt.New(t)
	pool := NewWeb3Pool()

	// Create a mock iterator with multiple endpoints
	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
		{ChainID: 1, URI: "http://endpoint3.example.com"},
	}

	pool.endpoints[1] = NewWeb3Iterator(endpoints...)

	// Verify initial state
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 3)

	// Disable first endpoint
	pool.DisableEndpoint(1, "http://endpoint1.example.com")
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 2)
	c.Assert(pool.NumberOfEndpoints(1, false), qt.Equals, 3)

	// Disable second endpoint
	pool.DisableEndpoint(1, "http://endpoint2.example.com")
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 1)

	// Disable third endpoint - should trigger reset
	pool.DisableEndpoint(1, "http://endpoint3.example.com")

	// After disabling all endpoints, they should be reset to available
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 3)
}

// TestDisableNonExistentEndpoint tests that disabling a non-existent endpoint
// doesn't cause issues (race condition fix)
func TestDisableNonExistentEndpoint(t *testing.T) {
	c := qt.New(t)
	pool := NewWeb3Pool()

	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
	}

	pool.endpoints[1] = NewWeb3Iterator(endpoints...)

	// Try to disable an endpoint that doesn't exist
	pool.DisableEndpoint(1, "http://nonexistent.example.com")

	// Should still have 2 available endpoints
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 2)

	// Try to disable from a chainID that doesn't exist
	pool.DisableEndpoint(999, "http://endpoint1.example.com")

	// Original chain should still have 2 endpoints
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 2)
}

// TestIteratorRoundRobin tests that the iterator properly cycles through endpoints
func TestIteratorRoundRobin(t *testing.T) {
	c := qt.New(t)
	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
		{ChainID: 1, URI: "http://endpoint3.example.com"},
	}

	iter := NewWeb3Iterator(endpoints...)

	// Get endpoints in round-robin fashion
	ep1, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep1.URI, qt.Equals, "http://endpoint1.example.com")

	ep2, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep2.URI, qt.Equals, "http://endpoint2.example.com")

	ep3, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep3.URI, qt.Equals, "http://endpoint3.example.com")

	// Should wrap around to first endpoint
	ep4, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep4.URI, qt.Equals, "http://endpoint1.example.com")
}

// TestIteratorDisableAndNext tests that disabling an endpoint properly updates the next index
func TestIteratorDisableAndNext(t *testing.T) {
	c := qt.New(t)
	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
		{ChainID: 1, URI: "http://endpoint3.example.com"},
	}

	iter := NewWeb3Iterator(endpoints...)

	// Get first endpoint (nextIndex moves to 1, pointing to endpoint2)
	ep1, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep1.URI, qt.Equals, "http://endpoint1.example.com")

	// Disable the second endpoint (at index 1, which is where nextIndex points)
	// After removal: [endpoint1, endpoint3], nextIndex stays at 1 but gets decremented to 0
	// because we removed an element before it
	iter.Disable("http://endpoint2.example.com")

	// Next should return endpoint1 (at index 0, since nextIndex was adjusted)
	// Then nextIndex moves to 1
	ep2, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep2.URI, qt.Equals, "http://endpoint1.example.com")

	// Next should return endpoint3 (at index 1)
	ep3, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep3.URI, qt.Equals, "http://endpoint3.example.com")
}

// TestIteratorDisableCurrentEndpoint tests disabling the current endpoint
func TestIteratorDisableCurrentEndpoint(t *testing.T) {
	c := qt.New(t)
	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
		{ChainID: 1, URI: "http://endpoint3.example.com"},
	}

	iter := NewWeb3Iterator(endpoints...)

	// Get first endpoint
	ep1, err := iter.Next()
	c.Assert(err, qt.IsNil)

	// Disable the first endpoint (the one we just got, but next index is now at 1)
	iter.Disable(ep1.URI)

	// Next should return endpoint2 (index 0 after removal, but nextIndex was adjusted)
	ep2, err := iter.Next()
	c.Assert(err, qt.IsNil)
	c.Assert(ep2.URI, qt.Equals, "http://endpoint2.example.com")
}

// TestIteratorEmptyPool tests behavior with no endpoints
func TestIteratorEmptyPool(t *testing.T) {
	c := qt.New(t)
	iter := NewWeb3Iterator()

	_, err := iter.Next()
	c.Assert(err, qt.Not(qt.IsNil))

	c.Assert(iter.Available(), qt.Equals, 0)
}

// TestIteratorAllDisabled tests that all endpoints get reset when all are disabled
func TestIteratorAllDisabled(t *testing.T) {
	c := qt.New(t)
	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
	}

	iter := NewWeb3Iterator(endpoints...)

	// Disable all endpoints
	iter.Disable("http://endpoint1.example.com")
	c.Assert(iter.Available(), qt.Equals, 1)

	iter.Disable("http://endpoint2.example.com")

	// Should have reset all to available
	c.Assert(iter.Available(), qt.Equals, 2)
	c.Assert(iter.Disabled(), qt.Equals, 0)
}

// TestConcurrentAccess tests that concurrent access to the iterator is safe
func TestConcurrentAccess(t *testing.T) {
	c := qt.New(t)
	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
		{ChainID: 1, URI: "http://endpoint3.example.com"},
	}

	iter := NewWeb3Iterator(endpoints...)

	// Run multiple goroutines accessing the iterator concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_, _ = iter.Next()
				time.Sleep(time.Microsecond)
			}
			done <- true
		}()
	}

	// Also disable endpoints concurrently
	go func() {
		for i := 0; i < 10; i++ {
			iter.Disable("http://endpoint1.example.com")
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}

	// Should still be in a valid state
	c.Assert(iter.Available() >= 0, qt.IsTrue)
}

// TestRetryLogic tests the retry logic with endpoint switching
func TestRetryLogic(t *testing.T) {
	c := qt.New(t)
	pool := NewWeb3Pool()

	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
	}

	pool.endpoints[1] = NewWeb3Iterator(endpoints...)

	client := &Client{
		w3p:     pool,
		chainID: 1,
	}

	// Test that retryAndCheckErr properly tracks endpoints
	callCount := 0
	testErr := errors.New("test error")

	_, err := client.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		callCount++
		// Fail for the first endpoint's retries
		if callCount <= defaultRetries {
			return nil, testErr
		}
		// Succeed on second endpoint
		return "success", nil
	})

	c.Assert(err, qt.IsNil)

	// Should have tried first endpoint 3 times, then succeeded on second endpoint
	c.Assert(callCount, qt.Equals, defaultRetries+1)
}

// TestRetryAllEndpointsFail tests that when all endpoints fail, proper error is returned
func TestRetryAllEndpointsFail(t *testing.T) {
	c := qt.New(t)
	pool := NewWeb3Pool()

	endpoints := []*Web3Endpoint{
		{ChainID: 1, URI: "http://endpoint1.example.com"},
		{ChainID: 1, URI: "http://endpoint2.example.com"},
	}

	pool.endpoints[1] = NewWeb3Iterator(endpoints...)

	client := &Client{
		w3p:     pool,
		chainID: 1,
	}

	testErr := errors.New("test error")

	_, err := client.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		return nil, testErr
	})

	c.Assert(err, qt.Not(qt.IsNil))

	// All endpoints should have been tried and disabled, then reset
	c.Assert(pool.NumberOfEndpoints(1, true), qt.Equals, 2)
}

// TestNoEndpointsAvailable tests behavior when no endpoints are configured
func TestNoEndpointsAvailable(t *testing.T) {
	c := qt.New(t)
	pool := NewWeb3Pool()

	client := &Client{
		w3p:     pool,
		chainID: 999, // Non-existent chain
	}

	_, err := client.retryAndCheckErr(func(endpoint *Web3Endpoint) (any, error) {
		return nil, nil
	})

	c.Assert(err, qt.Not(qt.IsNil))
}

// TestPoolInitialization tests that the pool is properly initialized
func TestPoolInitialization(t *testing.T) {
	c := qt.New(t)
	pool := NewWeb3Pool()

	c.Assert(pool.endpoints, qt.Not(qt.IsNil))
	c.Assert(len(pool.endpoints), qt.Equals, 0)
}
