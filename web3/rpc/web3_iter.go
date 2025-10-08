package rpc

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

// Web3Endpoint struct contains all the required information about a web3
// provider based on its URI. It includes its chain ID, its name (and shortName)
// and the URI.
type Web3Endpoint struct {
	ChainID   uint64 `json:"chainId"`
	URI       string
	IsArchive bool
	client    *ethclient.Client
	rpcClient *rpc.Client
}

const (
	endpointCooldownPeriod = 5 * time.Minute
)

// Web3Iterator struct is a pool of Web3Endpoint that allows to get the next
// available endpoint in a round-robin fashion. It also allows to disable an
// endpoint if it fails. It allows to manage multiple endpoints safely.
type Web3Iterator struct {
	nextIndex     int
	available     []*Web3Endpoint
	disabled      []*Web3Endpoint
	disabledUntil map[string]time.Time
	mtx           sync.Mutex
}

// NewWeb3Iterator creates a new Web3Iterator with the given endpoints.
func NewWeb3Iterator(endpoints ...*Web3Endpoint) *Web3Iterator {
	if endpoints == nil {
		endpoints = make([]*Web3Endpoint, 0)
	}
	return &Web3Iterator{
		available:     endpoints,
		disabled:      make([]*Web3Endpoint, 0),
		disabledUntil: make(map[string]time.Time),
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
// there are no registered endpoints, it will return an error. If there are no
// available endpoints due to all being in cooldown, it will return an error
// indicating when the next endpoint will be available.
func (w3pp *Web3Iterator) Next() (*Web3Endpoint, error) {
	if w3pp == nil {
		return nil, fmt.Errorf("nil Web3Iterator")
	}
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()

	l := len(w3pp.available)
	if l == 0 {
		if len(w3pp.disabled) > 0 {
			now := time.Now()
			var earliestAvailable time.Time
			for _, endpoint := range w3pp.disabled {
				if disabledUntil, exists := w3pp.disabledUntil[endpoint.URI]; exists {
					if earliestAvailable.IsZero() || disabledUntil.Before(earliestAvailable) {
						earliestAvailable = disabledUntil
					}
				}
			}
			if !earliestAvailable.IsZero() {
				timeUntilAvailable := earliestAvailable.Sub(now)
				return nil, fmt.Errorf("all endpoints are in cooldown, next available in %v", timeUntilAvailable)
			}
		}
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

// Disable method disables an endpoint, moving it from the available list to the
// disabled list with a cooldown period before it can be re-enabled.
func (w3pp *Web3Iterator) Disable(uri string) {
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()

	// Find the index of the endpoint to disable
	var index = -1
	for i, e := range w3pp.available {
		if e.URI == uri {
			index = i
			break
		}
	}

	// If endpoint not found in available list, return early
	if index == -1 {
		return
	}

	// Get the endpoint to disable and move it to the disabled list
	disabledEndpoint := w3pp.available[index]
	w3pp.available = append(w3pp.available[:index], w3pp.available[index+1:]...)
	w3pp.disabled = append(w3pp.disabled, disabledEndpoint)

	w3pp.disabledUntil[uri] = time.Now().Add(endpointCooldownPeriod)

	if w3pp.nextIndex == index {
		w3pp.nextIndex++
	}

	// If there are no available endpoints, check if any disabled endpoints
	// have completed their cooldown period
	if len(w3pp.available) == 0 {
		now := time.Now()
		var canReEnable []*Web3Endpoint
		var stillDisabled []*Web3Endpoint

		for _, endpoint := range w3pp.disabled {
			if disabledUntil, exists := w3pp.disabledUntil[endpoint.URI]; exists {
				if now.After(disabledUntil) {
					canReEnable = append(canReEnable, endpoint)
					delete(w3pp.disabledUntil, endpoint.URI)
				} else {
					stillDisabled = append(stillDisabled, endpoint)
				}
			} else {
				canReEnable = append(canReEnable, endpoint)
			}
		}

		w3pp.available = canReEnable
		w3pp.disabled = stillDisabled
		w3pp.nextIndex = 0
	}

	// Reset it to the first one
	if w3pp.nextIndex >= len(w3pp.available) {
		w3pp.nextIndex = 0
	}
}
