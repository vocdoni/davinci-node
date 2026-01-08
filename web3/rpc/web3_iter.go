package rpc

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
)

const (
	// endpointCooldownDuration is how long to wait before re-enabling a disabled endpoint
	endpointCooldownDuration = 5 * time.Minute
)

// Web3Endpoint struct contains all the required information about a web3
// provider based on its URI. It includes its chain ID, its name (and shortName)
// and the URI.
type Web3Endpoint struct {
	ChainID    uint64 `json:"chainId"`
	URI        string
	IsArchive  bool
	client     *ethclient.Client
	rpcClient  *gethrpc.Client
	disabledAt time.Time // When this endpoint was disabled (zero if never disabled)
}

// Web3Iterator struct is a pool of Web3Endpoint that allows to get the next
// available endpoint in a round-robin fashion. It also allows to disable an
// endpoint if it fails. It allows to manage multiple endpoints safely.
type Web3Iterator struct {
	nextIndex int
	available []*Web3Endpoint
	disabled  []*Web3Endpoint
	mtx       sync.Mutex
}

// NewWeb3Iterator creates a new Web3Iterator with the given endpoints.
func NewWeb3Iterator(endpoints ...*Web3Endpoint) *Web3Iterator {
	if endpoints == nil {
		endpoints = make([]*Web3Endpoint, 0)
	}
	return &Web3Iterator{
		available: endpoints,
		disabled:  make([]*Web3Endpoint, 0),
	}
}

// Available returns the number of available endpoints.
func (w3pp *Web3Iterator) Available() int {
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()
	return len(w3pp.available)
}

// Disabled returns the number of disabled endpoints.
func (w3pp *Web3Iterator) Disabled() int {
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()
	return len(w3pp.disabled)
}

// Add adds a new endpoint to the pool, making it available for the next
// requests.
func (w3pp *Web3Iterator) Add(endpoint ...*Web3Endpoint) {
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()
	w3pp.available = append(w3pp.available, endpoint...)
}

// Next returns the next available endpoint in a round-robin fashion. If
// there are no registered endpoints, it will return an error. It also checks
// for disabled endpoints that have passed their cooldown period and re-enables them.
func (w3pp *Web3Iterator) Next() (*Web3Endpoint, error) {
	if w3pp == nil {
		return nil, fmt.Errorf("nil Web3Iterator")
	}
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()

	// Check if any disabled endpoints have passed their cooldown period
	w3pp.checkCooldowns()

	l := len(w3pp.available)
	if l == 0 {
		return nil, fmt.Errorf("no registered endpoints")
	}
	// get the current endpoint. the next index can not be invalid at this
	// point because the available list not empty, the next index is always a
	// valid index since it is updated when an endpoint is disabled or when the
	// resulting endpoint is resolved, so the endpoint can not be nil
	currentEndpoint := w3pp.available[w3pp.nextIndex]
	// calculate the following next endpoint index based on the current one
	if w3pp.nextIndex++; w3pp.nextIndex >= l {
		// if the next index is out of bounds, reset it to the first one
		w3pp.nextIndex = 0
	}
	// update the next index and return the current endpoint
	return currentEndpoint, nil
}

// checkCooldowns checks if any disabled endpoints have passed their cooldown
// period and re-enables them. Must be called with mutex locked.
func (w3pp *Web3Iterator) checkCooldowns() {
	if len(w3pp.disabled) == 0 {
		return
	}

	now := time.Now()
	var stillDisabled []*Web3Endpoint

	for _, ep := range w3pp.disabled {
		// Check if cooldown period has passed
		if now.Sub(ep.disabledAt) >= endpointCooldownDuration {
			// Re-enable this endpoint
			ep.disabledAt = time.Time{} // Clear the disabled timestamp
			w3pp.available = append(w3pp.available, ep)
		} else {
			// Still in cooldown
			stillDisabled = append(stillDisabled, ep)
		}
	}

	w3pp.disabled = stillDisabled
}

// Disable method disables an endpoint, moving it from the available list to the
// the disabled list. It records the time when the endpoint was disabled for
// cooldown tracking.
func (w3pp *Web3Iterator) Disable(uri string) {
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()

	// Find the index of the endpoint to disable
	index := -1
	for i, e := range w3pp.available {
		if e.URI == uri {
			index = i
			break
		}
	}

	// If endpoint not found in available list, it may already be disabled or never existed
	if index == -1 {
		return
	}

	// Get the endpoint to disable and move it to the disabled list
	disabledEndpoint := w3pp.available[index]
	disabledEndpoint.disabledAt = time.Now() // Record when it was disabled
	w3pp.available = append(w3pp.available[:index], w3pp.available[index+1:]...)
	w3pp.disabled = append(w3pp.disabled, disabledEndpoint)

	// If the next index is the one we just disabled, update it to the next one
	if w3pp.nextIndex == index {
		w3pp.nextIndex++
	} else if w3pp.nextIndex > index {
		// If next index was after the disabled one, decrement it since we removed an element
		w3pp.nextIndex--
	}

	// If there are no available endpoints, reset all the disabled ones to
	// available ones and reset the next index to the first one
	if len(w3pp.available) == 0 {
		// Reset the next index and move the disabled endpoints to the available
		w3pp.nextIndex = 0
		w3pp.available = append(w3pp.available, w3pp.disabled...)
		w3pp.disabled = make([]*Web3Endpoint, 0)
		// Clear disabled timestamps since they're back in rotation
		for _, ep := range w3pp.available {
			ep.disabledAt = time.Time{}
		}
	} else if w3pp.nextIndex >= len(w3pp.available) {
		// If the next index is out of bounds, reset it to the first one
		w3pp.nextIndex = 0
	}
}
