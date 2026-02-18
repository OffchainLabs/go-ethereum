// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package pruner

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/holiman/uint256"
)

// collectTrieHashes walks the account trie and all storage tries rooted at the
// given state root, returning every internal trie node hash encountered.
func collectTrieHashes(t *testing.T, db *triedb.Database, root common.Hash) map[common.Hash]struct{} {
	t.Helper()
	hashes := make(map[common.Hash]struct{})

	sdb := state.NewDatabase(db, nil)
	tr, err := sdb.OpenTrie(root)
	if err != nil {
		t.Fatal("failed to open account trie:", err)
	}
	accountIt, err := tr.NodeIterator(nil)
	if err != nil {
		t.Fatal("failed to create account iterator:", err)
	}
	for accountIt.Next(true) {
		if h := accountIt.Hash(); h != (common.Hash{}) {
			hashes[h] = struct{}{}
		}
		if accountIt.Leaf() {
			var acct types.StateAccount
			if err := rlp.DecodeBytes(accountIt.LeafBlob(), &acct); err != nil {
				t.Fatal("failed to decode account:", err)
			}
			if acct.Root == types.EmptyRootHash || acct.Root == (common.Hash{}) {
				continue
			}
			key := common.BytesToHash(accountIt.LeafKey())
			storageTr, err := trie.NewStateTrie(
				trie.StorageTrieID(root, key, acct.Root),
				db,
			)
			if err != nil {
				t.Fatal("failed to open storage trie:", err)
			}
			storageIt, err := storageTr.NodeIterator(nil)
			if err != nil {
				t.Fatal("failed to create storage iterator:", err)
			}
			for storageIt.Next(true) {
				if h := storageIt.Hash(); h != (common.Hash{}) {
					hashes[h] = struct{}{}
				}
			}
			if storageIt.Error() != nil {
				t.Fatal("storage iterator error:", storageIt.Error())
			}
		}
	}
	if accountIt.Error() != nil {
		t.Fatal("account iterator error:", accountIt.Error())
	}
	return hashes
}

// TestParallelStorageTraversal verifies that dumpRawTrieDescendants produces
// identical bloom filter contents whether parallel storage traversal is enabled
// or disabled. This ensures the 32-range partitioning doesn't miss any trie nodes.
func TestParallelStorageTraversal(t *testing.T) {
	memDB := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(memDB, &triedb.Config{HashDB: &hashdb.Config{
		CleanCacheSize: 0,
	}})
	sdb := state.NewDatabase(tdb, nil)
	stateDB, err := state.New(types.EmptyRootHash, sdb)
	if err != nil {
		t.Fatal("failed to create state:", err)
	}

	// Create accounts with enough storage entries to produce internal trie
	// nodes that span multiple ranges in the 32-way parallel partition.
	for i := byte(0); i < 5; i++ {
		addr := common.BytesToAddress([]byte{i + 1})
		stateDB.AddBalance(addr, uint256.NewInt(1), tracing.BalanceChangeUnspecified)
		stateDB.SetNonce(addr, 1, tracing.NonceChangeUnspecified)
		for j := 0; j < 64; j++ {
			key := common.BytesToHash([]byte{i, byte(j)})
			val := common.BytesToHash([]byte{byte(j + 1)})
			stateDB.SetState(addr, key, val)
		}
	}

	root, err := stateDB.Commit(0, false, false)
	if err != nil {
		t.Fatal("failed to commit state:", err)
	}
	if err := tdb.Commit(root, false); err != nil {
		t.Fatal("failed to flush trie to disk:", err)
	}

	// Collect all trie node hashes by independent full iteration.
	allHashes := collectTrieHashes(t, tdb, root)
	if len(allHashes) == 0 {
		t.Fatal("expected non-empty trie")
	}
	t.Logf("total trie node hashes: %d", len(allHashes))

	// Run dumpRawTrieDescendants with parallel=false and parallel=true,
	// then verify both bloom filters contain every known hash.
	for _, parallel := range []bool{false, true} {
		bloom, err := newStateBloomWithSize(256)
		if err != nil {
			t.Fatal("failed to create bloom:", err)
		}
		config := &Config{
			BloomSize:                256,
			Threads:                  4,
			CleanCacheSize:           0,
			ParallelStorageTraversal: parallel,
		}
		if err := dumpRawTrieDescendants(memDB, root, bloom, config); err != nil {
			t.Fatalf("dumpRawTrieDescendants(parallel=%v) failed: %v", parallel, err)
		}
		for h := range allHashes {
			if !bloom.Contain(h.Bytes()) {
				t.Errorf("parallel=%v: bloom missing hash %s", parallel, h)
			}
		}
	}
}
