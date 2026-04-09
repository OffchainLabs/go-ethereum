// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// Key prefixes for eager preimage storage
	eagerPreimagePrefix    = []byte("eager-preimage-")    // + blockHash -> serializedEagerBlockRecord
	eagerBlockIndexPrefix  = []byte("eager-block-index-") // + blockNumber (8 bytes) -> blockHash
)

// EagerBlockRecord stores the preimages and user WASMs captured during a single
// block's production.
type EagerBlockRecord struct {
	Preimages        map[common.Hash][]byte
	UserWasms        state.UserWasms
	MinBlockAccessed uint64
}

// serializedPreimageEntry is used for RLP encoding of preimage map entries.
type serializedPreimageEntry struct {
	Hash common.Hash
	Blob []byte
}

// serializedWasmEntry is used for RLP encoding of WASM entries.
type serializedWasmEntry struct {
	ModuleHash common.Hash
	Targets    []serializedWasmTarget
}

type serializedWasmTarget struct {
	Target rawdb.WasmTarget
	Code   []byte
}

// serializedEagerBlockRecord is the RLP-encodable representation of EagerBlockRecord.
type serializedEagerBlockRecord struct {
	PreimageEntries  []serializedPreimageEntry
	WasmEntries      []serializedWasmEntry
	MinBlockAccessed uint64
}

func serializeRecord(record *EagerBlockRecord) (*serializedEagerBlockRecord, error) {
	preimages := make([]serializedPreimageEntry, 0, len(record.Preimages))
	for hash, blob := range record.Preimages {
		preimages = append(preimages, serializedPreimageEntry{Hash: hash, Blob: blob})
	}

	wasms := make([]serializedWasmEntry, 0, len(record.UserWasms))
	for moduleHash, activated := range record.UserWasms {
		targets := make([]serializedWasmTarget, 0, len(activated))
		for target, code := range activated {
			targets = append(targets, serializedWasmTarget{Target: target, Code: code})
		}
		wasms = append(wasms, serializedWasmEntry{ModuleHash: moduleHash, Targets: targets})
	}

	return &serializedEagerBlockRecord{
		PreimageEntries:  preimages,
		WasmEntries:      wasms,
		MinBlockAccessed: record.MinBlockAccessed,
	}, nil
}

func deserializeRecord(s *serializedEagerBlockRecord) *EagerBlockRecord {
	preimages := make(map[common.Hash][]byte, len(s.PreimageEntries))
	for _, entry := range s.PreimageEntries {
		preimages[entry.Hash] = entry.Blob
	}

	userWasms := make(state.UserWasms, len(s.WasmEntries))
	for _, entry := range s.WasmEntries {
		activated := make(state.ActivatedWasm, len(entry.Targets))
		for _, target := range entry.Targets {
			activated[target.Target] = target.Code
		}
		userWasms[entry.ModuleHash] = activated
	}

	return &EagerBlockRecord{
		Preimages:        preimages,
		UserWasms:        userWasms,
		MinBlockAccessed: s.MinBlockAccessed,
	}
}

// EagerPreimageStore provides persistent per-block preimage storage with an
// in-memory cache. It stores preimage maps captured during eager block recording
// and serves them when the validator requests them.
type EagerPreimageStore struct {
	db    ethdb.Database
	mu    sync.RWMutex
	cache map[common.Hash]*EagerBlockRecord // blockHash -> record (simple bounded cache)

	cacheSize int
}

// NewEagerPreimageStore creates a new EagerPreimageStore.
func NewEagerPreimageStore(db ethdb.Database, cacheSize int) *EagerPreimageStore {
	if cacheSize <= 0 {
		cacheSize = 128
	}
	return &EagerPreimageStore{
		db:        db,
		cache:     make(map[common.Hash]*EagerBlockRecord),
		cacheSize: cacheSize,
	}
}

func preimageKey(blockHash common.Hash) []byte {
	return append(eagerPreimagePrefix, blockHash.Bytes()...)
}

func blockIndexKey(blockNumber uint64) []byte {
	key := make([]byte, len(eagerBlockIndexPrefix)+8)
	copy(key, eagerBlockIndexPrefix)
	binary.BigEndian.PutUint64(key[len(eagerBlockIndexPrefix):], blockNumber)
	return key
}

// Store persists a block's preimage record to the database and cache.
func (s *EagerPreimageStore) Store(blockHash common.Hash, blockNumber uint64, record *EagerBlockRecord) error {
	serialized, err := serializeRecord(record)
	if err != nil {
		return fmt.Errorf("failed to serialize eager preimage record: %w", err)
	}
	encoded, err := rlp.EncodeToBytes(serialized)
	if err != nil {
		return fmt.Errorf("failed to RLP encode eager preimage record: %w", err)
	}

	batch := s.db.NewBatch()
	if err := batch.Put(preimageKey(blockHash), encoded); err != nil {
		return err
	}
	if err := batch.Put(blockIndexKey(blockNumber), blockHash.Bytes()); err != nil {
		return err
	}
	if err := batch.Write(); err != nil {
		return err
	}

	// Update cache
	s.mu.Lock()
	defer s.mu.Unlock()

	// Simple cache eviction: if we exceed cacheSize, clear the cache
	// (in production, an LRU would be better, but this is simple and correct)
	if len(s.cache) >= s.cacheSize {
		s.cache = make(map[common.Hash]*EagerBlockRecord)
	}
	s.cache[blockHash] = record

	return nil
}

// Get retrieves a block's preimage record from cache or database.
func (s *EagerPreimageStore) Get(blockHash common.Hash) (*EagerBlockRecord, error) {
	// Check cache first
	s.mu.RLock()
	if record, ok := s.cache[blockHash]; ok {
		s.mu.RUnlock()
		return record, nil
	}
	s.mu.RUnlock()

	// Read from database
	encoded, err := s.db.Get(preimageKey(blockHash))
	if err != nil {
		return nil, fmt.Errorf("eager preimage record not found for block %v: %w", blockHash, err)
	}

	var serialized serializedEagerBlockRecord
	if err := rlp.DecodeBytes(encoded, &serialized); err != nil {
		return nil, fmt.Errorf("failed to decode eager preimage record: %w", err)
	}

	record := deserializeRecord(&serialized)

	// Populate cache
	s.mu.Lock()
	if len(s.cache) >= s.cacheSize {
		s.cache = make(map[common.Hash]*EagerBlockRecord)
	}
	s.cache[blockHash] = record
	s.mu.Unlock()

	return record, nil
}

// Delete removes a block's preimage record from the database and cache.
func (s *EagerPreimageStore) Delete(blockHash common.Hash) error {
	if err := s.db.Delete(preimageKey(blockHash)); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, blockHash)
	s.mu.Unlock()
	return nil
}

// GarbageCollect removes preimage records for blocks older than keepAfterBlock.
func (s *EagerPreimageStore) GarbageCollect(keepAfterBlock uint64, getCanonicalHash func(uint64) common.Hash) error {
	if keepAfterBlock == 0 {
		return nil
	}

	batch := s.db.NewBatch()
	for blockNum := uint64(0); blockNum < keepAfterBlock; blockNum++ {
		indexKey := blockIndexKey(blockNum)
		hashBytes, err := s.db.Get(indexKey)
		if err != nil {
			continue // no record for this block
		}
		var blockHash common.Hash
		copy(blockHash[:], hashBytes)

		if err := batch.Delete(preimageKey(blockHash)); err != nil {
			return err
		}
		if err := batch.Delete(indexKey); err != nil {
			return err
		}

		s.mu.Lock()
		delete(s.cache, blockHash)
		s.mu.Unlock()

		// Flush in batches to avoid memory issues
		if batch.ValueSize() > 1024*1024 {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	return batch.Write()
}
