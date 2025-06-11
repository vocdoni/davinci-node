package sequencer

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
)

// ProcessIDMap provides a thread-safe map for storing and retrieving process IDs.
// It handles the conversion of process IDs from byte slices to hexadecimal strings internally.
type ProcessIDMap struct {
	data             map[string]time.Time // Last update time for each process ID
	firstBallotTimes map[string]time.Time // Timestamp of first ballot after last batch
	mu               sync.RWMutex
}

// NewProcessIDMap creates a new empty ProcessIDMap.
func NewProcessIDMap() *ProcessIDMap {
	return &ProcessIDMap{
		data:             make(map[string]time.Time),
		firstBallotTimes: make(map[string]time.Time),
	}
}

// Add adds a process ID to the map with the current time.
// If the process ID already exists, this operation has no effect.
// Returns true if the process ID was added, false if it already existed.
func (p *ProcessIDMap) Add(pid []byte) bool {
	if len(pid) == 0 {
		log.Warnw("attempted to add empty process ID")
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// If the process ID is already in the map, just return
	if _, exists := p.data[fmt.Sprintf("%x", pid)]; exists {
		return false
	}

	pidStr := fmt.Sprintf("%x", pid)
	p.data[pidStr] = time.Now()

	return true
}

// Remove removes a process ID from the map.
// If the process ID is not in the map, this operation has no effect.
// Returns true if the process ID was removed, false if it wasn't in the map.
func (p *ProcessIDMap) Remove(pid []byte) bool {
	if len(pid) == 0 {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	pidStr := fmt.Sprintf("%x", pid)
	delete(p.data, pidStr)
	return true
}

// Exists checks if a process ID is in the map.
func (p *ProcessIDMap) Exists(pid []byte) bool {
	if len(pid) == 0 {
		return false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	pidStr := fmt.Sprintf("%x", pid)
	_, exists := p.data[pidStr]
	return exists
}

// Get returns the time when a process ID was added and a boolean indicating
// whether the process ID exists in the map.
func (p *ProcessIDMap) Get(pid []byte) (time.Time, bool) {
	if len(pid) == 0 {
		return time.Time{}, false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	pidStr := fmt.Sprintf("%x", pid)
	t, exists := p.data[pidStr]
	return t, exists
}

// ForEach executes the given function for each process ID in the map.
// Takes a snapshot of the data and releases the lock before executing callbacks,
// ensuring long-running operations don't block other map operations.
func (p *ProcessIDMap) ForEach(f func(pid []byte, t time.Time) bool) {
	// Create a copy of the data we need to iterate over
	type pidItem struct {
		bytes []byte
		time  time.Time
	}

	// Take a lock only long enough to create a copy of the map data
	p.mu.RLock()
	items := make([]pidItem, 0, len(p.data))
	for pidStr, t := range p.data {
		pidBytes, err := hex.DecodeString(pidStr)
		if err != nil {
			// This should never happen if we're consistent with our hex encoding
			continue
		}
		items = append(items, pidItem{bytes: pidBytes, time: t})
	}
	p.mu.RUnlock() // Release the lock before executing callbacks

	// Process the copied data without holding the lock
	for _, item := range items {
		if !f(item.bytes, item.time) {
			break
		}
	}
}

// List returns a slice of all process IDs in the map as byte slices.
func (p *ProcessIDMap) List() [][]byte {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([][]byte, 0, len(p.data))
	for pidStr := range p.data {
		pidBytes, err := hex.DecodeString(pidStr)
		if err != nil {
			// This should never happen if we're consistent with our hex encoding
			continue
		}
		result = append(result, pidBytes)
	}
	return result
}

// Len returns the number of process IDs in the map.
func (p *ProcessIDMap) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return len(p.data)
}

// SetFirstBallotTime sets the timestamp for when the first ballot arrived
// after the last batch, but only if it hasn't been set already.
func (p *ProcessIDMap) SetFirstBallotTime(pid []byte) {
	if len(pid) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	pidStr := fmt.Sprintf("%x", pid)
	// Only set if not already set (we only care about the first ballot)
	if _, exists := p.firstBallotTimes[pidStr]; !exists {
		p.firstBallotTimes[pidStr] = time.Now()
	}
}

// GetFirstBallotTime returns the timestamp of when the first ballot arrived
// after the last batch processing, and a boolean indicating if it exists.
func (p *ProcessIDMap) GetFirstBallotTime(pid []byte) (time.Time, bool) {
	if len(pid) == 0 {
		return time.Time{}, false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	pidStr := fmt.Sprintf("%x", pid)
	t, exists := p.firstBallotTimes[pidStr]
	return t, exists
}

// ClearFirstBallotTime clears the first ballot timestamp for a process ID.
// This should be called after a batch is processed.
func (p *ProcessIDMap) ClearFirstBallotTime(pid []byte) {
	if len(pid) == 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	pidStr := fmt.Sprintf("%x", pid)
	delete(p.firstBallotTimes, pidStr)
}
