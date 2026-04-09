// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

// recordingNodeReader wraps a database.NodeReader and captures every trie node
// read as a hash→blob preimage entry. This works with both HashDB and PathDB
// backends because the Node() method receives the hash in both schemes.
type recordingNodeReader struct {
	inner     database.NodeReader
	collector *RecordingTrieDB
}

// Node implements database.NodeReader. It delegates to the inner reader and
// records the result keyed by hash.
func (r *recordingNodeReader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	blob, err := r.inner.Node(owner, path, hash)
	if err == nil && len(blob) > 0 {
		r.collector.record(hash, blob)
	}
	return blob, err
}

// RecordingTrieDB wraps a *triedb.Database and implements database.NodeDatabase.
// Every NodeReader it produces is wrapped to capture hash→blob mappings for all
// trie node reads. This is the core primitive that enables preimage capture
// during normal block production regardless of the underlying state scheme.
type RecordingTrieDB struct {
	inner     *triedb.Database
	mu        sync.Mutex
	preimages map[common.Hash][]byte
}

// NewRecordingTrieDB creates a new RecordingTrieDB wrapping the given triedb.
func NewRecordingTrieDB(inner *triedb.Database) *RecordingTrieDB {
	return &RecordingTrieDB{
		inner:     inner,
		preimages: make(map[common.Hash][]byte),
	}
}

// NodeReader implements database.NodeDatabase. It returns a recording wrapper
// around the real NodeReader for the given state root.
func (r *RecordingTrieDB) NodeReader(stateRoot common.Hash) (database.NodeReader, error) {
	reader, err := r.inner.NodeReader(stateRoot)
	if err != nil {
		return nil, err
	}
	return &recordingNodeReader{
		inner:     reader,
		collector: r,
	}, nil
}

// record stores a hash→blob preimage entry in a thread-safe manner.
func (r *RecordingTrieDB) record(hash common.Hash, blob []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.preimages[hash]; !exists {
		// Deep copy the blob since the trie database may reuse the buffer
		copied := make([]byte, len(blob))
		copy(copied, blob)
		r.preimages[hash] = copied
	}
}

// Preimages returns all captured hash→blob preimage entries. The returned map
// should not be modified by the caller.
func (r *RecordingTrieDB) Preimages() map[common.Hash][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[common.Hash][]byte, len(r.preimages))
	for k, v := range r.preimages {
		result[k] = v
	}
	return result
}

// Inner returns the underlying triedb.Database.
func (r *RecordingTrieDB) Inner() *triedb.Database {
	return r.inner
}
