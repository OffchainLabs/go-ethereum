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
	"encoding/binary"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/prque"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	txIndexerTail                           = metrics.NewRegisteredGauge("txindexer/tail", nil)
	txIndexerUnindexedBlocksHistogram       = metrics.NewRegisteredHistogram("txindexer/unindexed/blocks/count", nil, metrics.NewBoundedHistogramSample())
	txIndexerUnindexedTransactionsHistogram = metrics.NewRegisteredHistogram("txindexer/unindexed/transactions/count", nil, metrics.NewBoundedHistogramSample())
)

// InitDatabaseFromFreezer reinitializes an empty database from a previous batch
// of frozen ancient blocks. The method iterates over all the frozen blocks and
// injects into the database the block hash->number mappings.
func InitDatabaseFromFreezer(db ethdb.Database) {
	// If we can't access the freezer or it's empty, abort
	frozen, err := db.Ancients()
	if err != nil || frozen == 0 {
		return
	}
	var (
		batch  = db.NewBatch()
		start  = time.Now()
		logged = start.Add(-7 * time.Second) // Unindex during import is fast, don't double log
		hash   common.Hash
	)
	for i := uint64(0); i < frozen; {
		// We read 100K hashes at a time, for a total of 3.2M
		count := uint64(100_000)
		if i+count > frozen {
			count = frozen - i
		}
		data, err := db.AncientRange(ChainFreezerHashTable, i, count, 32*count)
		if err != nil {
			log.Crit("Failed to init database from freezer", "err", err)
		}
		for j, h := range data {
			number := i + uint64(j)
			hash = common.BytesToHash(h)
			WriteHeaderNumber(batch, hash, number)
			// If enough data was accumulated in memory or we're at the last block, dump to disk
			if batch.ValueSize() > ethdb.IdealBatchSize {
				if err := batch.Write(); err != nil {
					log.Crit("Failed to write data to db", "err", err)
				}
				batch.Reset()
			}
		}
		i += uint64(len(data))
		// If we've spent too much time already, notify the user of what we're doing
		if time.Since(logged) > 8*time.Second {
			log.Info("Initializing database from freezer", "total", frozen, "number", i, "hash", hash, "elapsed", common.PrettyDuration(time.Since(start)))
			logged = time.Now()
		}
	}
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write data to db", "err", err)
	}
	batch.Reset()

	WriteHeadHeaderHash(db, hash)
	WriteHeadFastBlockHash(db, hash)
	log.Info("Initialized database from freezer", "blocks", frozen, "elapsed", common.PrettyDuration(time.Since(start)))
}

type blockTxHashes struct {
	number uint64
	hashes []common.Hash
}

// iterateTransactions iterates over all transactions in the (canon) block
// number(s) given, and yields the hashes on a channel. If there is a signal
// received from interrupt channel, the iteration will be aborted and result
// channel will be closed.
func iterateTransactions(db ethdb.Database, from uint64, to uint64, reverse bool, interrupt chan struct{}, maxThreads int) chan *blockTxHashes {
	// One thread sequentially reads data from db
	type numberRlp struct {
		number uint64
		rlp    rlp.RawValue
	}
	if to == from {
		return nil
	}
	threads := to - from
	if maxThreads > 0 {
		if threads > uint64(maxThreads) {
			threads = uint64(maxThreads)
		}
	} else {
		if cpus := runtime.NumCPU(); threads > uint64(cpus) {
			threads = uint64(cpus)
		}
	}
	var (
		rlpCh    = make(chan *numberRlp, threads*2)     // we send raw rlp over this channel
		hashesCh = make(chan *blockTxHashes, threads*2) // send hashes over hashesCh
	)
	// lookup runs in one instance
	lookup := func() {
		n, end := from, to
		if reverse {
			n, end = to-1, from-1
		}
		defer close(rlpCh)
		for n != end {
			data := ReadCanonicalBodyRLP(db, n)
			// Feed the block to the aggregator, or abort on interrupt
			select {
			case rlpCh <- &numberRlp{n, data}:
			case <-interrupt:
				return
			}
			if reverse {
				n--
			} else {
				n++
			}
		}
	}
	// process runs in parallel
	var nThreadsAlive atomic.Int32
	nThreadsAlive.Store(int32(threads))
	process := func() {
		defer func() {
			// Last processor closes the result channel
			if nThreadsAlive.Add(-1) == 0 {
				close(hashesCh)
			}
		}()
		for data := range rlpCh {
			var body types.Body
			if err := rlp.DecodeBytes(data.rlp, &body); err != nil {
				log.Warn("Failed to decode block body", "block", data.number, "error", err)
				return
			}
			var hashes []common.Hash
			for _, tx := range body.Transactions {
				hashes = append(hashes, tx.Hash())
			}
			result := &blockTxHashes{
				hashes: hashes,
				number: data.number,
			}
			// Feed the block to the aggregator, or abort on interrupt
			select {
			case hashesCh <- result:
			case <-interrupt:
				return
			}
		}
	}
	go lookup() // start the sequential db accessor
	for i := 0; i < int(threads); i++ {
		go process()
	}
	return hashesCh
}

// indexTransactions creates txlookup indices of the specified block range.
//
// This function iterates canonical chain in reverse order, it has one main advantage:
// We can write tx index tail flag periodically even without the whole indexing
// procedure is finished. So that we can resume indexing procedure next time quickly.
//
// There is a passed channel, the whole procedure will be interrupted if any
// signal received.
func indexTransactions(db ethdb.Database, from uint64, to uint64, interrupt chan struct{}, hook func(uint64) bool, report bool, threads int) {
	// short circuit for invalid range
	if from >= to {
		return
	}
	var (
		hashesCh = iterateTransactions(db, from, to, true, interrupt, threads)
		batch    = db.NewBatch()
		start    = time.Now()
		logged   = start.Add(-7 * time.Second)

		// Since we iterate in reverse, we expect the first number to come
		// in to be [to-1]. Therefore, setting lastNum to means that the
		// queue gap-evaluation will work correctly
		lastNum     = to
		queue       = prque.New[int64, *blockTxHashes](nil)
		blocks, txs = 0, 0 // for stats reporting
	)
	for chanDelivery := range hashesCh {
		// Push the delivery into the queue and process contiguous ranges.
		// Since we iterate in reverse, so lower numbers have lower prio, and
		// we can use the number directly as prio marker
		queue.Push(chanDelivery, int64(chanDelivery.number))
		for !queue.Empty() {
			// If the next available item is gapped, return
			if _, priority := queue.Peek(); priority != int64(lastNum-1) {
				break
			}
			// For testing
			if hook != nil && !hook(lastNum-1) {
				break
			}
			// Next block available, pop it off and index it
			delivery := queue.PopItem()
			lastNum = delivery.number
			WriteTxLookupEntries(batch, delivery.number, delivery.hashes)
			blocks++
			txs += len(delivery.hashes)
			// If enough data was accumulated in memory or we're at the last block, dump to disk
			if batch.ValueSize() > ethdb.IdealBatchSize {
				WriteTxIndexTail(batch, lastNum) // Also write the tail here
				if err := batch.Write(); err != nil {
					log.Crit("Failed writing batch to db", "error", err)
					return
				}
				txIndexerTail.Update(int64(lastNum))
				batch.Reset()
			}
			// If we've spent too much time already, notify the user of what we're doing
			if time.Since(logged) > 8*time.Second {
				log.Info("Indexing transactions", "blocks", blocks, "txs", txs, "tail", lastNum, "total", to-from, "elapsed", common.PrettyDuration(time.Since(start)))
				logged = time.Now()
			}
		}
	}
	// Flush the new indexing tail and the last committed data. It can also happen
	// that the last batch is empty because nothing to index, but the tail has to
	// be flushed anyway.
	WriteTxIndexTail(batch, lastNum)
	if err := batch.Write(); err != nil {
		log.Crit("Failed writing batch to db", "error", err)
		return
	}
	txIndexerTail.Update(int64(lastNum))
	logger := log.Debug
	if report {
		logger = log.Info
	}
	select {
	case <-interrupt:
		logger("Transaction indexing interrupted", "blocks", blocks, "txs", txs, "tail", lastNum, "elapsed", common.PrettyDuration(time.Since(start)))
	default:
		logger("Indexed transactions", "blocks", blocks, "txs", txs, "tail", lastNum, "elapsed", common.PrettyDuration(time.Since(start)))
	}
}

// IndexTransactions creates txlookup indices of the specified block range. The from
// is included while to is excluded.
//
// This function iterates canonical chain in reverse order, it has one main advantage:
// We can write tx index tail flag periodically even without the whole indexing
// procedure is finished. So that we can resume indexing procedure next time quickly.
//
// There is a passed channel, the whole procedure will be interrupted if any
// signal received.
func IndexTransactions(db ethdb.Database, from uint64, to uint64, interrupt chan struct{}, report bool, threads int) {
	indexTransactions(db, from, to, interrupt, nil, report, threads)
}

// indexTransactionsForTesting is the internal debug version with an additional hook.
func indexTransactionsForTesting(db ethdb.Database, from uint64, to uint64, interrupt chan struct{}, hook func(uint64) bool) {
	indexTransactions(db, from, to, interrupt, hook, false, 0)
}

// unindexTransactions removes txlookup indices of the specified block range.
//
// There is a passed channel, the whole procedure will be interrupted if any
// signal received.
func unindexTransactions(db ethdb.Database, from uint64, to uint64, interrupt chan struct{}, hook func(uint64) bool, report bool, threads int) {
	// short circuit for invalid range
	if from >= to {
		return
	}
	var (
		hashesCh = iterateTransactions(db, from, to, false, interrupt, threads)
		batch    = db.NewBatch()
		start    = time.Now()
		logged   = start.Add(-7 * time.Second)

		// we expect the first number to come in to be [from]. Therefore, setting
		// nextNum to from means that the queue gap-evaluation will work correctly
		nextNum     = from
		queue       = prque.New[int64, *blockTxHashes](nil)
		blocks, txs = 0, 0 // for stats reporting
	)
	// Otherwise spin up the concurrent iterator and unindexer
	for delivery := range hashesCh {
		// Push the delivery into the queue and process contiguous ranges.
		queue.Push(delivery, -int64(delivery.number))
		for !queue.Empty() {
			// If the next available item is gapped, return
			if _, priority := queue.Peek(); -priority != int64(nextNum) {
				break
			}
			// For testing
			if hook != nil && !hook(nextNum) {
				break
			}
			delivery := queue.PopItem()
			nextNum = delivery.number + 1
			DeleteTxLookupEntries(batch, delivery.hashes)
			txs += len(delivery.hashes)
			blocks++

			// If enough data was accumulated in memory or we're at the last block, dump to disk
			// A batch counts the size of deletion as '1', so we need to flush more
			// often than that.
			if blocks%1000 == 0 {
				WriteTxIndexTail(batch, nextNum)
				if err := batch.Write(); err != nil {
					log.Crit("Failed writing batch to db", "error", err)
					return
				}
				txIndexerTail.Update(int64(nextNum))
				batch.Reset()
			}
			// If we've spent too much time already, notify the user of what we're doing
			if time.Since(logged) > 8*time.Second {
				log.Info("Unindexing transactions", "blocks", blocks, "txs", txs, "total", to-from, "elapsed", common.PrettyDuration(time.Since(start)))
				logged = time.Now()
			}
		}
	}
	// Flush the new indexing tail and the last committed data. It can also happen
	// that the last batch is empty because nothing to unindex, but the tail has to
	// be flushed anyway.
	WriteTxIndexTail(batch, nextNum)
	if err := batch.Write(); err != nil {
		log.Crit("Failed writing batch to db", "error", err)
		return
	}
	txIndexerTail.Update(int64(nextNum))
	logger := log.Debug
	if report {
		logger = log.Info
	}
	select {
	case <-interrupt:
		logger("Transaction unindexing interrupted", "blocks", blocks, "txs", txs, "tail", to, "elapsed", common.PrettyDuration(time.Since(start)))
	default:
		logger("Unindexed transactions", "blocks", blocks, "txs", txs, "tail", to, "elapsed", common.PrettyDuration(time.Since(start)))
	}
	txIndexerUnindexedBlocksHistogram.Update(int64(blocks))
	txIndexerUnindexedTransactionsHistogram.Update(int64(txs))
}

// UnindexTransactions removes txlookup indices of the specified block range.
// The from is included while to is excluded.
//
// There is a passed channel, the whole procedure will be interrupted if any
// signal received.
func UnindexTransactions(db ethdb.Database, from uint64, to uint64, interrupt chan struct{}, report bool, threads int) {
	unindexTransactions(db, from, to, interrupt, nil, report, threads)
}

// unindexTransactionsForTesting is the internal debug version with an additional hook.
func unindexTransactionsForTesting(db ethdb.Database, from uint64, to uint64, interrupt chan struct{}, hook func(uint64) bool) {
	unindexTransactions(db, from, to, interrupt, hook, false, 0)
}

// PruneTransactionIndex removes all tx index entries below a certain block number.
func PruneTransactionIndex(db ethdb.Database, pruneBlock uint64) {
	tail := ReadTxIndexTail(db)
	if tail == nil || *tail > pruneBlock {
		return // no index, or index ends above pruneBlock
	}
	// There are blocks below pruneBlock in the index. Iterate the entire index to remove
	// their entries. Note if this fails, the index is messed up, but tail still points to
	// the old tail.
	var count, removed int
	DeleteAllTxLookupEntries(db, func(txhash common.Hash, v []byte) bool {
		count++
		if count%10000000 == 0 {
			log.Info("Pruning tx index", "count", count, "removed", removed)
		}
		if len(v) > 8 {
			log.Error("Skipping legacy tx index entry", "hash", txhash)
			return false
		}
		bn := decodeNumber(v)
		if bn < pruneBlock {
			removed++
			return true
		}
		return false
	})
	WriteTxIndexTail(db, pruneBlock)
}

func decodeNumber(b []byte) uint64 {
	var numBuffer [8]byte
	copy(numBuffer[8-len(b):], b)
	return binary.BigEndian.Uint64(numBuffer[:])
}
