package rpc

import (
	"fmt"
	"sync"

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
// there are no registered endpoints, it will return an error. If there are no
// available endpoints, it will reset the disabled endpoints and return the
// first available endpoint.
func (w3pp *Web3Iterator) Next() (*Web3Endpoint, error) {
	if w3pp == nil {
		return nil, fmt.Errorf("nil Web3Iterator")
	}
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()
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

// Disable method disables an endpoint, moving it from the available list to the
// the disabled list.
func (w3pp *Web3Iterator) Disable(uri string) {
	w3pp.mtx.Lock()
	defer w3pp.mtx.Unlock()
	// get the index of the endpoint to disable
	var index int
	for i, e := range w3pp.available {
		if e.URI == uri {
			index = i
			break
		}
	}
	// get the endpoint to disable and move it to the disabled list
	disabledEndpoint := w3pp.available[index]
	w3pp.available = append(w3pp.available[:index], w3pp.available[index+1:]...)
	w3pp.disabled = append(w3pp.disabled, disabledEndpoint)
	// if the next index is the one to disable, update it to the next one
	if w3pp.nextIndex == index {
		w3pp.nextIndex++
	}
	// if there are no available endpoints, reset all the disabled ones to
	// available ones and reset the next index to the first one
	if l := len(w3pp.available); l == 0 {
		// reset the next index and move the disabled endpoints to the available
		w3pp.nextIndex = 0
		w3pp.available = append(w3pp.available, w3pp.disabled...)
		w3pp.disabled = make([]*Web3Endpoint, 0)
	}
	// if the next index is out of bounds, reset it to the first one
	if w3pp.nextIndex >= len(w3pp.available) {
		w3pp.nextIndex = 0
	}
}
