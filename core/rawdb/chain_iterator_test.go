// Copyright 2020 The go-ethereum Authors
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

package rawdb

import (
	"math/big"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
)

func TestChainIterator(t *testing.T) {
	// Construct test chain db
	chainDb := NewMemoryDatabase()

	var block *types.Block
	var txs []*types.Transaction
	to := common.BytesToAddress([]byte{0x11})
	block = types.NewBlock(&types.Header{Number: big.NewInt(int64(0))}, nil, nil, newTestHasher()) // Empty genesis block
	WriteBlock(chainDb, block)
	WriteCanonicalHash(chainDb, block.Hash(), block.NumberU64())
	for i := uint64(1); i <= 10; i++ {
		var tx *types.Transaction
		if i%2 == 0 {
			tx = types.NewTx(&types.LegacyTx{
				Nonce:    i,
				GasPrice: big.NewInt(11111),
				Gas:      1111,
				To:       &to,
				Value:    big.NewInt(111),
				Data:     []byte{0x11, 0x11, 0x11},
			})
		} else {
			tx = types.NewTx(&types.AccessListTx{
				ChainID:  big.NewInt(1337),
				Nonce:    i,
				GasPrice: big.NewInt(11111),
				Gas:      1111,
				To:       &to,
				Value:    big.NewInt(111),
				Data:     []byte{0x11, 0x11, 0x11},
			})
		}
		txs = append(txs, tx)
		block = types.NewBlock(&types.Header{Number: big.NewInt(int64(i))}, &types.Body{Transactions: types.Transactions{tx}}, nil, newTestHasher())
		WriteBlock(chainDb, block)
		WriteCanonicalHash(chainDb, block.Hash(), block.NumberU64())
	}

	var cases = []struct {
		from, to uint64
		reverse  bool
		expect   []int
	}{
		{0, 11, true, []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}},
		{0, 0, true, nil},
		{0, 5, true, []int{4, 3, 2, 1, 0}},
		{10, 11, true, []int{10}},
		{0, 11, false, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{0, 0, false, nil},
		{10, 11, false, []int{10}},
	}
	for i, c := range cases {
		var numbers []int
		hashCh := iterateTransactions(chainDb, c.from, c.to, c.reverse, nil, 0)
		if hashCh != nil {
			for h := range hashCh {
				numbers = append(numbers, int(h.number))
				if len(h.hashes) > 0 {
					if got, exp := h.hashes[0], txs[h.number-1].Hash(); got != exp {
						t.Fatalf("block %d: hash wrong, got %x exp %x", h.number, got, exp)
					}
				}
			}
		}
		if !c.reverse {
			sort.Ints(numbers)
		} else {
			sort.Sort(sort.Reverse(sort.IntSlice(numbers)))
		}
		if !reflect.DeepEqual(numbers, c.expect) {
			t.Fatalf("Case %d failed, visit element mismatch, want %v, got %v", i, c.expect, numbers)
		}
	}
}

func initDatabaseWithTransactions(db ethdb.Database) ([]*types.Block, []*types.Transaction) {
	var blocks []*types.Block
	var txs []*types.Transaction
	to := common.BytesToAddress([]byte{0x11})

	// Write empty genesis block
	block := types.NewBlock(&types.Header{Number: big.NewInt(int64(0))}, nil, nil, newTestHasher())
	WriteBlock(db, block)
	WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	blocks = append(blocks, block)

	// Create transactions.
	for i := uint64(1); i <= 10; i++ {
		var tx *types.Transaction
		if i%2 == 0 {
			tx = types.NewTx(&types.LegacyTx{
				Nonce:    i,
				GasPrice: big.NewInt(11111),
				Gas:      1111,
				To:       &to,
				Value:    big.NewInt(111),
				Data:     []byte{0x11, 0x11, 0x11},
			})
		} else {
			tx = types.NewTx(&types.AccessListTx{
				ChainID:  big.NewInt(1337),
				Nonce:    i,
				GasPrice: big.NewInt(11111),
				Gas:      1111,
				To:       &to,
				Value:    big.NewInt(111),
				Data:     []byte{0x11, 0x11, 0x11},
			})
		}
		txs = append(txs, tx)
		block := types.NewBlock(&types.Header{Number: big.NewInt(int64(i))}, &types.Body{Transactions: types.Transactions{tx}}, nil, newTestHasher())
		WriteBlock(db, block)
		WriteCanonicalHash(db, block.Hash(), block.NumberU64())
		blocks = append(blocks, block)
	}

	return blocks, txs
}

func TestIndexTransactions(t *testing.T) {
	// Construct test chain db
	chainDB := NewMemoryDatabase()

	_, txs := initDatabaseWithTransactions(chainDB)

	// verify checks whether the tx indices in the range [from, to)
	// is expected.
	verify := func(from, to int, exist bool, tail uint64) {
		for i := from; i < to; i++ {
			if i == 0 {
				continue
			}
			number := ReadTxLookupEntry(chainDB, txs[i-1].Hash())
			if exist && number == nil {
				t.Fatalf("Transaction index %d missing", i)
			}
			if !exist && number != nil {
				t.Fatalf("Transaction index %d is not deleted", i)
			}
		}
		number := ReadTxIndexTail(chainDB)
		if number == nil || *number != tail {
			t.Fatalf("Transaction tail mismatch")
		}
	}
	IndexTransactions(chainDB, 5, 11, nil, false, 0)
	verify(5, 11, true, 5)
	verify(0, 5, false, 5)

	IndexTransactions(chainDB, 0, 5, nil, false, 0)
	verify(0, 11, true, 0)

	UnindexTransactions(chainDB, 0, 5, nil, false, 0)
	verify(5, 11, true, 5)
	verify(0, 5, false, 5)

	UnindexTransactions(chainDB, 5, 11, nil, false, 0)
	verify(0, 11, false, 11)

	// Testing corner cases
	signal := make(chan struct{})
	var once sync.Once
	indexTransactionsForTesting(chainDB, 5, 11, signal, func(n uint64) bool {
		if n <= 8 {
			once.Do(func() {
				close(signal)
			})
			return false
		}
		return true
	})
	verify(9, 11, true, 9)
	verify(0, 9, false, 9)
	IndexTransactions(chainDB, 0, 9, nil, false, 0)

	signal = make(chan struct{})
	var once2 sync.Once
	unindexTransactionsForTesting(chainDB, 0, 11, signal, func(n uint64) bool {
		if n >= 8 {
			once2.Do(func() {
				close(signal)
			})
			return false
		}
		return true
	})
	verify(8, 11, true, 8)
	verify(0, 8, false, 8)
}

func TestPruneTransactionIndex(t *testing.T) {
	chainDB := NewMemoryDatabase()
	blocks, _ := initDatabaseWithTransactions(chainDB)
	lastBlock := blocks[len(blocks)-1].NumberU64()
	pruneBlock := lastBlock - 3

	IndexTransactions(chainDB, 0, lastBlock+1, nil, false, 0)

	// Check all transactions are in index.
	for _, block := range blocks {
		for _, tx := range block.Transactions() {
			num := ReadTxLookupEntry(chainDB, tx.Hash())
			if num == nil || *num != block.NumberU64() {
				t.Fatalf("wrong TxLookup entry: %x -> %v", tx.Hash(), num)
			}
		}
	}

	PruneTransactionIndex(chainDB, pruneBlock)

	// Check transactions from old blocks not included.
	for _, block := range blocks {
		for _, tx := range block.Transactions() {
			num := ReadTxLookupEntry(chainDB, tx.Hash())
			if block.NumberU64() < pruneBlock && num != nil {
				t.Fatalf("TxLookup entry not removed: %x -> %v", tx.Hash(), num)
			}
			if block.NumberU64() >= pruneBlock && (num == nil || *num != block.NumberU64()) {
				t.Fatalf("wrong TxLookup entry after pruning: %x -> %v", tx.Hash(), num)
			}
		}
	}
}
