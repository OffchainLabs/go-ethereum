// Copyright 2022 The go-ethereum Authors
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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb/eradb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

const (
	// freezerRecheckInterval is the frequency to check the key-value database for
	// chain progression that might permit new blocks to be frozen into immutable
	// storage.
	freezerRecheckInterval = time.Minute

	// freezerBatchLimit is the maximum number of blocks to freeze in one batch
	// before doing an fsync and deleting it from the key-value store.
	freezerBatchLimit = 30000

	// freezerCleanupMargin is the number of blocks to keep in the key-value
	// database after they have been frozen into the ancient store. After an
	// unclean shutdown, repair() may truncate the freezer head to restore
	// cross-table consistency. Retaining these blocks in the key-value store
	// allows freezeRange() to re-freeze them via nofreezedb. Without this
	// margin, a crash could leave blocks missing from both stores, making the
	// node unable to start (especially for L2 nodes that cannot re-sync
	// pruned blocks from peers).
	//
	// Set to freezerBatchLimit as a practical heuristic: each freeze cycle
	// writes at most that many blocks before calling SyncAncient (flush), so
	// the unflushed window per cycle — and thus the typical repair()
	// truncation distance — is bounded by this value. Corruption beyond
	// this margin triggers the startup check in Open() and halts the node.
	freezerCleanupMargin = freezerBatchLimit
)

// chainFreezer is a wrapper of chain ancient store with additional chain freezing
// feature. The background thread will keep moving ancient chain segments from
// key-value database to flat files for saving space on live database.
type chainFreezer struct {
	ancients ethdb.AncientStore // Ancient store for storing cold chain segment

	// Optional Era database used as a backup for the pruned chain.
	eradb *eradb.Store

	// cleanupMargin is the number of most-recently-frozen blocks that remain
	// in the KV store after being copied to the ancient store. Cleanup
	// incrementally deletes frozen blocks from KV up to frozen-cleanupMargin,
	// tracking progress via the persisted cleanup tail.
	// Defaults to freezerCleanupMargin; overridden in tests.
	// Must be set before freeze() is started and not modified afterward.
	cleanupMargin uint64

	quit    chan struct{}
	wg      sync.WaitGroup
	trigger chan chan struct{} // Manual blocking freeze trigger, test determinism
}

// newChainFreezer initializes the freezer for ancient chain segment.
//
//   - if the empty directory is given, initializes the pure in-memory
//     state freezer (e.g. dev mode).
//   - if non-empty directory is given, initializes the regular file-based
//     state freezer.
func newChainFreezer(datadir string, eraDir string, namespace string, readonly bool) (*chainFreezer, error) {
	if datadir == "" {
		return &chainFreezer{
			ancients:      NewMemoryFreezer(readonly, chainFreezerTableConfigs),
			cleanupMargin: freezerCleanupMargin,
			quit:          make(chan struct{}),
			trigger:       make(chan chan struct{}),
		}, nil
	}
	freezer, err := NewFreezer(datadir, namespace, readonly, freezerTableSize, chainFreezerTableConfigs)
	if err != nil {
		return nil, err
	}
	edb, err := eradb.New(resolveChainEraDir(datadir, eraDir))
	if err != nil {
		return nil, err
	}
	return &chainFreezer{
		ancients:      freezer,
		eradb:         edb,
		cleanupMargin: freezerCleanupMargin,
		quit:          make(chan struct{}),
		trigger:       make(chan chan struct{}),
	}, nil
}

// Close closes the chain freezer instance and terminates the background thread.
func (f *chainFreezer) Close() error {
	select {
	case <-f.quit:
	default:
		close(f.quit)
	}
	f.wg.Wait()

	if f.eradb != nil {
		f.eradb.Close()
	}
	return f.ancients.Close()
}

// readHeadNumber returns the number of chain head block. 0 is returned if the
// block is unknown or not available yet.
func (f *chainFreezer) readHeadNumber(db ethdb.KeyValueReader) uint64 {
	hash := ReadHeadBlockHash(db)
	if hash == (common.Hash{}) {
		log.Warn("Head block is not reachable")
		return 0
	}
	number, ok := ReadHeaderNumber(db, hash)
	if !ok {
		log.Error("Number of head block is missing")
		return 0
	}
	return number
}

// readFinalizedNumber returns the number of finalized block. 0 is returned
// if the block is unknown or not available yet.
func (f *chainFreezer) readFinalizedNumber(db ethdb.KeyValueReader) uint64 {
	hash := ReadFinalizedBlockHash(db)
	if hash == (common.Hash{}) {
		return 0
	}
	number, ok := ReadHeaderNumber(db, hash)
	if !ok {
		log.Error("Number of finalized block is missing")
		return 0
	}
	return number
}

// freezeThreshold returns the threshold for chain freezing. It's determined
// by formula: max(finality, HEAD-params.FullImmutabilityThreshold).
func (f *chainFreezer) freezeThreshold(db ethdb.KeyValueReader) (uint64, error) {
	var (
		head      = f.readHeadNumber(db)
		final     = f.readFinalizedNumber(db)
		headLimit uint64
	)
	if head > params.FullImmutabilityThreshold {
		headLimit = head - params.FullImmutabilityThreshold
	}
	if final == 0 && headLimit == 0 {
		return 0, errors.New("freezing threshold is not available")
	}
	if final > headLimit {
		return final, nil
	}
	return headLimit, nil
}

// freeze is a background thread that periodically checks the blockchain for any
// import progress and moves ancient data from the fast database into the freezer.
//
// This functionality is deliberately broken off from block importing to avoid
// incurring additional data shuffling delays on block propagation.
func (f *chainFreezer) freeze(db ethdb.KeyValueStore) {
	var (
		backoff   bool
		triggered chan struct{} // Used in tests
		nfdb      = &nofreezedb{KeyValueStore: db}
	)
	timer := time.NewTimer(freezerRecheckInterval)
	defer timer.Stop()

	for {
		select {
		case <-f.quit:
			log.Info("Freezer shutting down")
			return
		default:
		}
		if backoff {
			// If we were doing a manual trigger, notify it
			if triggered != nil {
				triggered <- struct{}{}
				triggered = nil
			}
			select {
			case <-timer.C:
				backoff = false
				timer.Reset(freezerRecheckInterval)
			case triggered = <-f.trigger:
				backoff = false
			case <-f.quit:
				return
			}
		}
		threshold, err := f.freezeThreshold(nfdb)
		if err != nil {
			backoff = true
			log.Debug("Current full block not old enough to freeze", "err", err)
			continue
		}
		frozen, _ := f.Ancients() // no error will occur, safe to ignore

		// Short circuit if the blocks below threshold are already frozen.
		if frozen != 0 && frozen-1 >= threshold {
			backoff = true
			log.Debug("Ancient blocks frozen already", "threshold", threshold, "frozen", frozen)
			continue
		}
		// Seems we have data ready to be frozen, process in usable batches
		var (
			start = time.Now()
			first = frozen    // the first block to freeze
			last  = threshold // the last block to freeze
		)
		if last-first+1 > freezerBatchLimit {
			last = freezerBatchLimit + first - 1
		}
		ancients, err := f.freezeRange(nfdb, first, last)
		if err != nil {
			log.Error("Error in block freeze operation", "err", err)
			backoff = true
			continue
		}
		// Batch of blocks have been frozen, flush them before wiping from key-value store
		if err := f.SyncAncient(); err != nil {
			log.Crit("Failed to flush frozen tables", "err", err)
		}
		// Delete frozen blocks from the key-value store, stopping cleanupMargin
		// blocks behind the freezer head so repair()-truncated entries remain
		// available for re-freezing.
		frozen, err = f.Ancients()
		if err != nil {
			log.Error("Failed to read frozen count after sync, skipping cleanup", "err", err)
			backoff = true
			continue
		}
		var cleanupLimit uint64
		if frozen > f.cleanupMargin {
			cleanupLimit = frozen - f.cleanupMargin
		}
		// Determine where cleanup should start. Genesis (block 0) is always
		// kept in the key-value store for Open() genesis hash validation.
		prev, prevOk, prevErr := readFreezerCleanupTailStrict(db)
		if prevErr != nil {
			log.Error("Failed to read cleanup tail, skipping cleanup", "err", prevErr)
			backoff = true
			continue
		}
		var cleanupStart uint64
		switch {
		case prevOk:
			cleanupStart = max(prev, 1)
		case cleanupLimit == 0:
			// Nothing to clean up yet (frozen <= margin).
		case frozen > params.FullImmutabilityThreshold:
			// Significant frozen history but no cleanup tail suggests upgrade
			// from code that predates the safety margin feature. Skip ahead
			// to cleanupLimit since those blocks were already deleted by old code.
			cleanupStart = cleanupLimit
			log.Warn("Cleanup tail missing despite significant frozen history (upgrade from old code?)",
				"frozen", frozen, "newTail", cleanupStart)
		default:
			// Fresh install: clean from block 1 so early blocks don't
			// remain in the KV store permanently. Per-cycle cap prevents stalling.
			cleanupStart = 1
			log.Info("Initialized freezer cleanup tail (first run with safety margin)", "tail", cleanupStart)
		}
		// Cap per-cycle work to avoid stalling when cleanup has a large backlog
		// (e.g., first run after upgrade).
		if cleanupLimit > cleanupStart+freezerBatchLimit {
			cleanupLimit = cleanupStart + freezerBatchLimit
		}
		// Wipe out all data from the active database. Has()+Get() distinguishes
		// missing keys from I/O errors to avoid advancing the tail past
		// unverifiable blocks.
		batch := db.NewBatch()
		var skipped uint64
		for number := cleanupStart; number < cleanupLimit; number++ {
			key := headerHashKey(number)
			exists, err := db.Has(key)
			if err != nil {
				log.Error("I/O error during cleanup, aborting cycle",
					"number", number, "err", err)
				cleanupLimit = number // don't advance tail past unreadable blocks
				backoff = true
				break
			}
			if !exists {
				skipped++
				continue
			}
			hashData, err := db.Get(key)
			if err != nil {
				// Has() succeeded but Get() failed — transient I/O error.
				log.Error("Failed to read canonical hash during cleanup",
					"number", number, "err", err)
				cleanupLimit = number
				backoff = true
				break
			}
			hash := common.BytesToHash(hashData)
			DeleteBlockWithoutNumber(batch, hash, number)
			DeleteCanonicalHash(batch, number)
		}
		if skipped > 0 {
			logFn := log.Info
			if skipped == cleanupLimit-cleanupStart {
				logFn = log.Warn // All blocks missing is unusual.
			}
			logFn("Skipped cleanup of blocks already absent from KV (normal after crash recovery)",
				"count", skipped, "start", cleanupStart, "limit", cleanupLimit)
		}
		if err := batch.Write(); err != nil {
			log.Crit("Failed to delete frozen canonical blocks", "err", err)
		}
		batch.Reset()

		// Wipe out side chains also and track dangling side chains
		var dangling []common.Hash
		for number := cleanupStart; number < cleanupLimit; number++ {
			dangling = ReadAllHashes(db, number)
			for _, hash := range dangling {
				log.Trace("Deleting side chain", "number", number, "hash", hash)
				DeleteBlock(batch, hash, number)
			}
		}
		if err := batch.Write(); err != nil {
			log.Crit("Failed to delete frozen side blocks", "err", err)
		}
		batch.Reset()

		// Step into the future and delete any dangling side chains.
		// Bound the chase to avoid unbounded iteration on corrupted data.
		tip := cleanupLimit
		for danglingDepth := 0; len(dangling) > 0 && danglingDepth < freezerBatchLimit; danglingDepth++ {
			drop := make(map[common.Hash]struct{})
			for _, hash := range dangling {
				log.Debug("Dangling parent from Freezer", "number", tip-1, "hash", hash)
				drop[hash] = struct{}{}
			}
			children := ReadAllHashes(db, tip)
			for i := 0; i < len(children); i++ {
				// Dig up the child and ensure it's dangling
				child := ReadHeader(nfdb, children[i], tip)
				if child == nil {
					log.Error("Missing dangling header", "number", tip, "hash", children[i])
					continue
				}
				if _, ok := drop[child.ParentHash]; !ok {
					children = append(children[:i], children[i+1:]...)
					i--
					continue
				}
				// Delete all block data associated with the child
				log.Debug("Deleting dangling block", "number", tip, "hash", children[i], "parent", child.ParentHash)
				DeleteBlock(batch, children[i], tip)
			}
			dangling = children
			tip++
		}
		if len(dangling) > 0 {
			log.Error("Dangling side chain chase exceeded maximum depth, remaining orphans may persist",
				"remaining", len(dangling), "depth", freezerBatchLimit, "tip", tip)
		}
		// Persist the cleanup tail with the side chain batch. A crash between
		// the canonical batch and this one is harmless: the Has() check in the
		// cleanup loop skips already-deleted blocks on re-iteration.
		// Write when cleanup made progress (madeProgress), or on first-time
		// init including the upgrade path where cleanupStart == cleanupLimit
		// (firstInit). Skip if frozen regressed (cleanupLimit < cleanupStart).
		madeProgress := cleanupLimit > cleanupStart
		firstInit := cleanupLimit > 0 && !prevOk
		if madeProgress || firstInit {
			WriteFreezerCleanupTail(batch, cleanupLimit)
		} else if cleanupLimit > 0 && cleanupLimit < cleanupStart {
			log.Warn("Skipping cleanup tail update: frozen count regressed (will self-heal once freezer recovers)",
				"cleanupLimit", cleanupLimit, "cleanupStart", cleanupStart)
		}
		if err := batch.Write(); err != nil {
			log.Crit("Failed to delete dangling side blocks", "err", err)
		}

		// Log something friendly for the user
		context := []interface{}{
			"blocks", frozen - first, "elapsed", common.PrettyDuration(time.Since(start)), "number", frozen - 1,
		}
		if n := len(ancients); n > 0 {
			context = append(context, []interface{}{"hash", ancients[n-1]}...)
		}
		log.Debug("Deep froze chain segment", context...)

		// Avoid database thrashing with tiny writes
		if frozen-first < freezerBatchLimit {
			backoff = true
		}
	}
}

// freezeRange moves a batch of chain segments from the fast database to the freezer.
// The parameters (number, limit) specify the relevant block range, both of which
// are included.
func (f *chainFreezer) freezeRange(nfdb *nofreezedb, number, limit uint64) (hashes []common.Hash, err error) {
	hashes = make([]common.Hash, 0, limit-number+1)

	_, err = f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		for ; number <= limit; number++ {
			// Retrieve all the components of the canonical block.
			hash := ReadCanonicalHash(nfdb, number)
			if hash == (common.Hash{}) {
				return fmt.Errorf("canonical hash missing, can't freeze block %d", number)
			}
			header := ReadHeaderRLP(nfdb, hash, number)
			if len(header) == 0 {
				return fmt.Errorf("block header missing, can't freeze block %d", number)
			}
			body := ReadBodyRLP(nfdb, hash, number)
			if len(body) == 0 {
				return fmt.Errorf("block body missing, can't freeze block %d", number)
			}
			receipts := ReadReceiptsRLP(nfdb, hash, number)
			if len(receipts) == 0 {
				return fmt.Errorf("block receipts missing, can't freeze block %d", number)
			}
			// Write to the batch.
			if err := op.AppendRaw(ChainFreezerHashTable, number, hash[:]); err != nil {
				return fmt.Errorf("can't write hash to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerHeaderTable, number, header); err != nil {
				return fmt.Errorf("can't write header to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerBodiesTable, number, body); err != nil {
				return fmt.Errorf("can't write body to Freezer: %v", err)
			}
			if err := op.AppendRaw(ChainFreezerReceiptTable, number, receipts); err != nil {
				return fmt.Errorf("can't write receipts to Freezer: %v", err)
			}
			hashes = append(hashes, hash)
		}
		return nil
	})
	return hashes, err
}

// Ancient retrieves an ancient binary blob from the append-only immutable files.
func (f *chainFreezer) Ancient(kind string, number uint64) ([]byte, error) {
	// Lookup the entry in the underlying ancient store, assuming that
	// headers and hashes are always available.
	if kind == ChainFreezerHeaderTable || kind == ChainFreezerHashTable {
		return f.ancients.Ancient(kind, number)
	}
	tail, err := f.ancients.Tail()
	if err != nil {
		return nil, err
	}
	// Lookup the entry in the underlying ancient store if it's not pruned
	if number >= tail {
		return f.ancients.Ancient(kind, number)
	}
	// Lookup the entry in the optional era backend
	if f.eradb == nil {
		return nil, errOutOfBounds
	}
	switch kind {
	case ChainFreezerBodiesTable:
		return f.eradb.GetRawBody(number)
	case ChainFreezerReceiptTable:
		return f.eradb.GetRawReceipts(number)
	}
	return nil, errUnknownTable
}

// ReadAncients executes an operation while preventing mutations to the freezer,
// i.e. if fn performs multiple reads, they will be consistent with each other.
func (f *chainFreezer) ReadAncients(fn func(ethdb.AncientReaderOp) error) (err error) {
	if store, ok := f.ancients.(*Freezer); ok {
		store.writeLock.Lock()
		defer store.writeLock.Unlock()
	}
	return fn(f)
}

// Methods below are just pass-through to the underlying ancient store.

func (f *chainFreezer) Ancients() (uint64, error) {
	return f.ancients.Ancients()
}

func (f *chainFreezer) Tail() (uint64, error) {
	return f.ancients.Tail()
}

func (f *chainFreezer) AncientSize(kind string) (uint64, error) {
	return f.ancients.AncientSize(kind)
}

func (f *chainFreezer) AncientRange(kind string, start, count, maxBytes uint64) ([][]byte, error) {
	return f.ancients.AncientRange(kind, start, count, maxBytes)
}

func (f *chainFreezer) AncientBytes(kind string, id, offset, length uint64) ([]byte, error) {
	return f.ancients.AncientBytes(kind, id, offset, length)
}

func (f *chainFreezer) ModifyAncients(fn func(ethdb.AncientWriteOp) error) (int64, error) {
	return f.ancients.ModifyAncients(fn)
}

func (f *chainFreezer) TruncateHead(items uint64) (uint64, error) {
	return f.ancients.TruncateHead(items)
}

func (f *chainFreezer) TruncateTail(items uint64) (uint64, error) {
	return f.ancients.TruncateTail(items)
}

func (f *chainFreezer) SyncAncient() error {
	return f.ancients.SyncAncient()
}
