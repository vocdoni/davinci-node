package inmemory

import (
	"bytes"
	"fmt"
	"slices"
	"sync"

	"github.com/vocdoni/davinci-node/db"
)

type entry struct {
	value   []byte
	version uint64
	deleted bool
}

// InMemoryDB implements an ephemeral in-memory db.Database.
type InMemoryDB struct {
	mu          sync.RWMutex
	data        map[string]entry
	nextVersion uint64
}

// Ensure that InMemoryDB implements the db.Database interface.
var _ db.Database = (*InMemoryDB)(nil)

// New returns a new in-memory database. Options are ignored.
func New(_ db.Options) (*InMemoryDB, error) {
	return &InMemoryDB{
		data: make(map[string]entry),
	}, nil
}

func (d *InMemoryDB) Close() error {
	return nil
}

func (d *InMemoryDB) Compact() error {
	return nil
}

func (d *InMemoryDB) WriteTx() db.WriteTx {
	d.mu.RLock()
	baseVer := d.nextVersion
	d.mu.RUnlock()
	return &WriteTx{
		db:      d,
		writes:  make(map[string]*[]byte),
		reads:   make(map[string]uint64),
		baseVer: baseVer,
	}
}

func (d *InMemoryDB) Get(key []byte) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ent, ok := d.data[string(key)]
	if !ok || ent.deleted {
		return nil, db.ErrKeyNotFound
	}
	return bytes.Clone(ent.value), nil
}

func (d *InMemoryDB) Iterate(prefix []byte, callback func(key, value []byte) bool) error {
	d.mu.RLock()
	entries := make(map[string][]byte, len(d.data))
	for k, ent := range d.data {
		if ent.deleted {
			continue
		}
		if !bytes.HasPrefix([]byte(k), prefix) {
			continue
		}
		entries[k] = bytes.Clone(ent.value)
	}
	d.mu.RUnlock()
	return iterateEntries(entries, callback)
}

func (d *InMemoryDB) currentVersion(key string) uint64 {
	ent, ok := d.data[key]
	if !ok {
		return 0
	}
	return ent.version
}

func (d *InMemoryDB) applyWrite(key string, value []byte, deleteKey bool) {
	d.nextVersion++
	ent := d.data[key]
	ent.version = d.nextVersion
	ent.deleted = deleteKey
	if deleteKey {
		ent.value = nil
	} else {
		ent.value = bytes.Clone(value)
	}
	d.data[key] = ent
}

type WriteTx struct {
	db        *InMemoryDB
	writes    map[string]*[]byte
	reads     map[string]uint64
	baseVer   uint64
	committed bool
	discarded bool
}

// Ensure that WriteTx implements the db.WriteTx interface.
var _ db.WriteTx = (*WriteTx)(nil)

func (tx *WriteTx) recordRead(key string, version uint64) {
	if _, ok := tx.reads[key]; ok {
		return
	}
	tx.reads[key] = version
}

func (tx *WriteTx) Get(key []byte) ([]byte, error) {
	strKey := string(key)
	if pending, ok := tx.writes[strKey]; ok {
		if pending == nil {
			return nil, db.ErrKeyNotFound
		}
		return bytes.Clone(*pending), nil
	}

	tx.db.mu.RLock()
	ent, ok := tx.db.data[strKey]
	version := tx.db.currentVersion(strKey)
	tx.db.mu.RUnlock()

	tx.recordRead(strKey, version)
	if !ok || ent.deleted {
		return nil, db.ErrKeyNotFound
	}
	return bytes.Clone(ent.value), nil
}

func (tx *WriteTx) Iterate(prefix []byte, callback func(k, v []byte) bool) error {
	tx.db.mu.RLock()
	entries := make(map[string][]byte, len(tx.db.data))
	readVersions := make(map[string]uint64, len(tx.db.data))
	for k, ent := range tx.db.data {
		if ent.deleted {
			continue
		}
		if !bytes.HasPrefix([]byte(k), prefix) {
			continue
		}
		entries[k] = bytes.Clone(ent.value)
		readVersions[k] = ent.version
	}
	tx.db.mu.RUnlock()

	for k, v := range tx.writes {
		if !bytes.HasPrefix([]byte(k), prefix) {
			continue
		}
		if v == nil {
			delete(entries, k)
			continue
		}
		entries[k] = bytes.Clone(*v)
	}

	for k, ver := range readVersions {
		tx.recordRead(k, ver)
	}

	return iterateEntries(entries, callback)
}

func (tx *WriteTx) Set(key, value []byte) error {
	strKey := string(key)
	if _, ok := tx.reads[strKey]; !ok {
		tx.db.mu.RLock()
		version := tx.db.currentVersion(strKey)
		tx.db.mu.RUnlock()
		tx.recordRead(strKey, version)
	}
	valCopy := bytes.Clone(value)
	tx.writes[strKey] = &valCopy
	return nil
}

func (tx *WriteTx) Delete(key []byte) error {
	strKey := string(key)
	if _, ok := tx.reads[strKey]; !ok {
		tx.db.mu.RLock()
		version := tx.db.currentVersion(strKey)
		tx.db.mu.RUnlock()
		tx.recordRead(strKey, version)
	}
	tx.writes[strKey] = nil
	return nil
}

func (tx *WriteTx) Apply(other db.WriteTx) error {
	return other.Iterate(nil, func(k, v []byte) bool {
		if err := tx.Set(k, v); err != nil {
			return false
		}
		return true
	})
}

func (tx *WriteTx) Commit() error {
	if tx.committed || tx.discarded {
		return fmt.Errorf("cannot commit inmemory tx: already committed or discarded")
	}

	tx.db.mu.Lock()
	defer tx.db.mu.Unlock()

	for key, readVersion := range tx.reads {
		current := tx.db.currentVersion(key)
		if readVersion > tx.baseVer || current != readVersion {
			return db.ErrConflict
		}
	}

	for key, value := range tx.writes {
		if value == nil {
			tx.db.applyWrite(key, nil, true)
			continue
		}
		tx.db.applyWrite(key, *value, false)
	}
	tx.committed = true
	return nil
}

func (tx *WriteTx) Discard() {
	tx.writes = map[string]*[]byte{}
	tx.reads = map[string]uint64{}
	tx.discarded = true
}

func iterateEntries(entries map[string][]byte, callback func(key, value []byte) bool) error {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	for _, key := range keys {
		if !callback([]byte(key), entries[key]) {
			break
		}
	}
	return nil
}
