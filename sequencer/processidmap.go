package sequencer

import (
	"sync"
	"time"

	"github.com/vocdoni/davinci-node/log"
	"github.com/vocdoni/davinci-node/types"
)

// ProcessIDMap provides a thread-safe map for storing and retrieving process IDs.
// It handles the conversion of process IDs from byte slices to hexadecimal strings internally.
type ProcessIDMap struct {
	data             map[types.ProcessID]time.Time // Last update time for each process ID
	firstBallotTimes map[types.ProcessID]time.Time // Timestamp of first ballot after last batch
	mu               sync.RWMutex
}

// NewProcessIDMap creates a new empty ProcessIDMap.
func NewProcessIDMap() *ProcessIDMap {
	return &ProcessIDMap{
		data:             make(map[types.ProcessID]time.Time),
		firstBallotTimes: make(map[types.ProcessID]time.Time),
	}
}

// Add adds a process ID to the map with the current time.
// If the process ID already exists, this operation has no effect.
// Returns true if the process ID was added, false if it already existed.
func (p *ProcessIDMap) Add(processID types.ProcessID) bool {
	if !processID.IsValid() {
		log.Warnw("attempted to add invalid process ID")
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// If the process ID is already in the map, just return
	if _, exists := p.data[processID]; exists {
		return false
	}

	p.data[processID] = time.Now()

	return true
}

// Remove removes a process ID from the map.
// If the process ID is not in the map, this operation has no effect.
// Returns true if the process ID was removed, false if it wasn't in the map.
func (p *ProcessIDMap) Remove(processID types.ProcessID) bool {
	if !processID.IsValid() {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.data, processID)
	return true
}

// Exists checks if a process/ ID is in the map.
func (p *ProcessIDMap) Exists(processID types.ProcessID) bool {
	if !processID.IsValid() {
		return false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	_, exists := p.data[processID]
	return exists
}

// Get returns the time when a process ID was added and a boolean indicating
// whether the process ID exists in the map.
func (p *ProcessIDMap) Get(processID types.ProcessID) (time.Time, bool) {
	if !processID.IsValid() {
		return time.Time{}, false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	t, exists := p.data[processID]
	return t, exists
}

// ForEach executes the given function for each process ID in the map.
// Takes a snapshot of the data and releases the lock before executing callbacks,
// ensuring long-running operations don't block other map operations.
func (p *ProcessIDMap) ForEach(f func(processID types.ProcessID, t time.Time) bool) {
	// Create a copy of the data we need to iterate over
	type pidItem struct {
		pid  types.ProcessID
		time time.Time
	}

	// Take a lock only long enough to create a copy of the map data
	p.mu.RLock()
	items := make([]pidItem, 0, len(p.data))
	for pid, t := range p.data {
		items = append(items, pidItem{pid: pid, time: t})
	}
	p.mu.RUnlock() // Release the lock before executing callbacks

	// Process the copied data without holding the lock
	for _, item := range items {
		if !f(item.pid, item.time) {
			break
		}
	}
}

// List returns a slice of all process IDs in the map as byte slices.
func (p *ProcessIDMap) List() []types.ProcessID {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]types.ProcessID, 0, len(p.data))
	for processID := range p.data {
		result = append(result, processID)
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
func (p *ProcessIDMap) SetFirstBallotTime(processID types.ProcessID) {
	if !processID.IsValid() {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Only set if not already set (we only care about the first ballot)
	if _, exists := p.firstBallotTimes[processID]; !exists {
		p.firstBallotTimes[processID] = time.Now()
	}
}

// GetFirstBallotTime returns the timestamp of when the first ballot arrived
// after the last batch processing, and a boolean indicating if it exists.
func (p *ProcessIDMap) GetFirstBallotTime(processID types.ProcessID) (time.Time, bool) {
	if !processID.IsValid() {
		return time.Time{}, false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	t, exists := p.firstBallotTimes[processID]
	return t, exists
}

// ClearFirstBallotTime clears the first ballot timestamp for a process ID.
// This should be called after a batch is processed.
func (p *ProcessIDMap) ClearFirstBallotTime(processID types.ProcessID) {
	if !processID.IsValid() {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.firstBallotTimes, processID)
}
