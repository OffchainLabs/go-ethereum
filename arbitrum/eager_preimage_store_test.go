// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
)

func makeTestRecord(id byte) *EagerBlockRecord {
	hash := common.Hash{}
	hash[0] = id
	return &EagerBlockRecord{
		Preimages: map[common.Hash][]byte{
			hash: {id, id + 1, id + 2},
		},
		UserWasms:        make(state.UserWasms),
		MinBlockAccessed: uint64(id),
	}
}

func TestEagerPreimageStoreRoundTrip(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	store := NewEagerPreimageStore(db, 10)

	blockHash := common.HexToHash("0xaaaa")
	blockNum := uint64(42)
	record := makeTestRecord(1)

	// Store
	err := store.Store(blockHash, blockNum, record)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Get from cache
	got, err := store.Get(blockHash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(got.Preimages) != len(record.Preimages) {
		t.Fatalf("preimage count mismatch: got %d, want %d", len(got.Preimages), len(record.Preimages))
	}
	if got.MinBlockAccessed != record.MinBlockAccessed {
		t.Fatalf("MinBlockAccessed mismatch: got %d, want %d", got.MinBlockAccessed, record.MinBlockAccessed)
	}

	// Verify preimage data
	for k, v := range record.Preimages {
		gotV, ok := got.Preimages[k]
		if !ok {
			t.Fatalf("missing preimage key %v", k)
		}
		if string(gotV) != string(v) {
			t.Fatalf("preimage value mismatch for key %v", k)
		}
	}
}

func TestEagerPreimageStoreGetFromDB(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	store := NewEagerPreimageStore(db, 10)

	blockHash := common.HexToHash("0xbbbb")
	blockNum := uint64(100)
	record := makeTestRecord(2)

	err := store.Store(blockHash, blockNum, record)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Create a new store with the same DB to force reading from DB (not cache)
	store2 := NewEagerPreimageStore(db, 10)
	got, err := store2.Get(blockHash)
	if err != nil {
		t.Fatalf("Get from DB failed: %v", err)
	}

	if got.MinBlockAccessed != record.MinBlockAccessed {
		t.Fatalf("MinBlockAccessed mismatch: got %d, want %d", got.MinBlockAccessed, record.MinBlockAccessed)
	}
	if len(got.Preimages) != len(record.Preimages) {
		t.Fatalf("preimage count mismatch: got %d, want %d", len(got.Preimages), len(record.Preimages))
	}
}

func TestEagerPreimageStoreDelete(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	store := NewEagerPreimageStore(db, 10)

	blockHash := common.HexToHash("0xcccc")
	blockNum := uint64(200)
	record := makeTestRecord(3)

	err := store.Store(blockHash, blockNum, record)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	err = store.Delete(blockHash)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Should not be found
	_, err = store.Get(blockHash)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestEagerPreimageStoreGarbageCollect(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	store := NewEagerPreimageStore(db, 100)

	// Store records for blocks 0-9
	blockHashes := make([]common.Hash, 10)
	for i := uint64(0); i < 10; i++ {
		hash := common.Hash{}
		hash[0] = byte(i)
		blockHashes[i] = hash
		record := makeTestRecord(byte(i))
		err := store.Store(hash, i, record)
		if err != nil {
			t.Fatalf("Store block %d failed: %v", i, err)
		}
	}

	// GC everything before block 5
	err := store.GarbageCollect(5, func(num uint64) common.Hash {
		if num < 10 {
			return blockHashes[num]
		}
		return common.Hash{}
	})
	if err != nil {
		t.Fatalf("GarbageCollect failed: %v", err)
	}

	// Blocks 0-4 should be gone
	for i := uint64(0); i < 5; i++ {
		_, err := store.Get(blockHashes[i])
		if err == nil {
			t.Fatalf("block %d should have been garbage collected", i)
		}
	}

	// Blocks 5-9 should still exist
	for i := uint64(5); i < 10; i++ {
		_, err := store.Get(blockHashes[i])
		if err != nil {
			t.Fatalf("block %d should still exist: %v", i, err)
		}
	}
}

func TestEagerPreimageStoreWithWasms(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	store := NewEagerPreimageStore(db, 10)

	blockHash := common.HexToHash("0xdddd")
	blockNum := uint64(42)

	moduleHash := common.HexToHash("0xeeee")
	record := &EagerBlockRecord{
		Preimages: map[common.Hash][]byte{
			common.HexToHash("0x1111"): {1, 2, 3},
		},
		UserWasms: state.UserWasms{
			moduleHash: state.ActivatedWasm{
				rawdb.WasmTarget("arm64"): []byte("arm64-code"),
				rawdb.WasmTarget("amd64"): []byte("amd64-code"),
			},
		},
		MinBlockAccessed: 10,
	}

	err := store.Store(blockHash, blockNum, record)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Read back from a fresh store (force DB read)
	store2 := NewEagerPreimageStore(db, 10)
	got, err := store2.Get(blockHash)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(got.UserWasms) != 1 {
		t.Fatalf("expected 1 wasm module, got %d", len(got.UserWasms))
	}

	activated, ok := got.UserWasms[moduleHash]
	if !ok {
		t.Fatal("missing module hash in user wasms")
	}
	if len(activated) != 2 {
		t.Fatalf("expected 2 wasm targets, got %d", len(activated))
	}
	if string(activated[rawdb.WasmTarget("arm64")]) != "arm64-code" {
		t.Fatal("arm64 code mismatch")
	}
	if string(activated[rawdb.WasmTarget("amd64")]) != "amd64-code" {
		t.Fatal("amd64 code mismatch")
	}
}

func TestEagerPreimageStoreCacheEviction(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// Cache size of 3
	store := NewEagerPreimageStore(db, 3)

	// Store 5 blocks — should trigger cache eviction
	blockHashes := make([]common.Hash, 5)
	for i := uint64(0); i < 5; i++ {
		hash := common.Hash{}
		hash[0] = byte(i + 10)
		blockHashes[i] = hash
		record := makeTestRecord(byte(i + 10))
		err := store.Store(hash, i, record)
		if err != nil {
			t.Fatalf("Store block %d failed: %v", i, err)
		}
	}

	// All blocks should still be retrievable (from cache or DB)
	for i := uint64(0); i < 5; i++ {
		_, err := store.Get(blockHashes[i])
		if err != nil {
			t.Fatalf("Get block %d failed after cache eviction: %v", i, err)
		}
	}
}
