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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
)

const (
	// stateBloomFileName is the filename of state bloom filter.
	stateBloomFileName = "statebloom.bf.gz"

	// stateBloomFileTempSuffix is the filename suffix of state bloom filter
	// while it is being written out to detect write aborts.
	stateBloomFileTempSuffix = ".tmp"

	// rangeCompactionThreshold is the minimal deleted entry number for
	// triggering range compaction. It's a quite arbitrary number but just
	// to avoid triggering range compaction because of small deletion.
	rangeCompactionThreshold = 100000
)

// Config includes all the configurations for pruning.
type Config struct {
	Datadir        string // The directory of the state database
	BloomSize      uint64 // The Megabytes of memory allocated to bloom-filter
	Threads        int    // The maximum number of threads spawned in dumpRawTrieDescendants and removeOtherRoots
	CleanCacheSize int    // The Megabytes of clean cache size used in dumpRawTrieDescendants
}

// Pruner is an offline tool to prune the stale state with the
// help of the snapshot. The workflow of pruner is very simple:
//
//   - iterate the snapshot, reconstruct the relevant state
//   - iterate the database, delete all other state entries which
//     don't belong to the target state and the genesis state
//
// It can take several hours(around 2 hours for mainnet) to finish
// the whole pruning work. It's recommended to run this offline tool
// periodically in order to release the disk usage and improve the
// disk read performance to some extent.
type Pruner struct {
	config      Config
	chainHeader *types.Header
	db          ethdb.Database
	stateBloom  *stateBloom
	snaptree    *snapshot.Tree
}

// NewPruner creates the pruner instance.
func NewPruner(db ethdb.Database, config Config) (*Pruner, error) {
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return nil, errors.New("failed to load head block")
	}
	// Offline pruning is only supported in legacy hash based scheme.
	triedb := triedb.NewDatabase(db, triedb.HashDefaults)

	snapconfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   false,
		NoBuild:    true,
		AsyncBuild: false,
	}
	snaptree, err := snapshot.New(snapconfig, db, triedb, headBlock.Root())
	if err != nil {
		return nil, err // The relevant snapshot(s) might not exist
	}
	// Sanitize the bloom filter size if it's too small.
	if config.BloomSize < 256 {
		log.Warn("Sanitizing bloomfilter size", "provided(MB)", config.BloomSize, "updated(MB)", 256)
		config.BloomSize = 256
	}
	stateBloom, err := newStateBloomWithSize(config.BloomSize)
	if err != nil {
		return nil, err
	}
	// sanitize threads number, if set too low
	if config.Threads <= 0 {
		config.Threads = 1
	}
	return &Pruner{
		config:      config,
		chainHeader: headBlock.Header(),
		db:          db,
		stateBloom:  stateBloom,
		snaptree:    snaptree,
	}, nil
}

func readStoredChainConfig(db ethdb.Database) *params.ChainConfig {
	block0Hash := rawdb.ReadCanonicalHash(db, 0)
	if block0Hash == (common.Hash{}) {
		return nil
	}
	return rawdb.ReadChainConfig(db, block0Hash)
}

func removeOtherRoots(db ethdb.Database, rootsList []common.Hash, stateBloom *stateBloom, threads int) error {
	chainConfig := readStoredChainConfig(db)
	var genesisBlockNum uint64
	if chainConfig != nil {
		genesisBlockNum = chainConfig.ArbitrumChainParams.GenesisBlockNum
	}
	roots := make(map[common.Hash]struct{})
	for _, root := range rootsList {
		roots[root] = struct{}{}
	}
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return errors.New("failed to load head block")
	}
	blockRange := headBlock.NumberU64() - genesisBlockNum
	var wg sync.WaitGroup
	errors := make(chan error, threads)
	for thread := 0; thread < threads; thread++ {
		thread := thread
		wg.Add(1)
		go func() {
			defer wg.Done()
			firstBlockNum := blockRange/uint64(threads)*uint64(thread+1) + genesisBlockNum
			if thread == threads-1 {
				firstBlockNum = headBlock.NumberU64()
			}
			endBlockNum := blockRange/uint64(threads)*uint64(thread) + genesisBlockNum
			if thread != 0 {
				// endBlockNum is the last block that will be checked
				endBlockNum++
			}
			startedAt := time.Now()
			lastLog := time.Now()
			firstBlockHash := rawdb.ReadCanonicalHash(db, firstBlockNum)
			block := rawdb.ReadBlock(db, firstBlockHash, firstBlockNum)
			for {
				if block == nil || block.Root() == (common.Hash{}) {
					return
				}
				bloomContains := stateBloom.Contain(block.Root().Bytes())
				if bloomContains {
					_, rootsContains := roots[block.Root()]
					if !rootsContains {
						log.Info(
							"Found false positive state root bloom filter match",
							"blockNum", block.Number(),
							"blockHash", block.Hash(),
							"stateRoot", block.Root(),
						)
						// This state root is a false positive of the bloom filter
						err := db.Delete(block.Root().Bytes())
						if err != nil {
							errors <- err
							return
						}
					}
				}
				if block.NumberU64() <= endBlockNum {
					return
				}
				if thread == threads-1 && time.Since(lastLog) >= time.Second*30 {
					lastLog = time.Now()
					elapsed := time.Since(startedAt)
					totalWork := float32(firstBlockNum - endBlockNum)
					completedBlocks := float32(block.NumberU64() - endBlockNum)
					log.Info("Removing old state roots", "elapsed", elapsed, "eta", time.Duration(float32(elapsed)*(totalWork/completedBlocks))-elapsed)
				}
				block = rawdb.ReadBlock(db, block.ParentHash(), block.NumberU64()-1)
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-errors:
		return err
	default:
		log.Info("Done removing old state roots")
		return nil
	}
}

// Arbitrum: snaptree and root are for the final snapshot kept
func prune(snaptree *snapshot.Tree, allRoots []common.Hash, maindb ethdb.Database, stateBloom *stateBloom, bloomPath string, start time.Time, threads int) error {
	// Delete all stale trie nodes in the disk. With the help of state bloom
	// the trie nodes(and codes) belong to the active state will be filtered
	// out. A very small part of stale tries will also be filtered because of
	// the false-positive rate of bloom filter. But the assumption is held here
	// that the false-positive is low enough(~0.05%). The probability of the
	// dangling node is the state root is super low. So the dangling nodes in
	// theory will never ever be visited again.
	var (
		skipped, count int
		size           common.StorageSize
		pstart         = time.Now()
		logged         = time.Now()
		batch          = maindb.NewBatch()
		iter           = maindb.NewIterator(nil, nil)
	)
	log.Info("Loaded state bloom filter", "sizeMB", stateBloom.Size()/(1024*1024), "falsePositiveProbability", stateBloom.FalsePosititveProbability())
	for iter.Next() {
		key := iter.Key()

		// All state entries don't belong to specific state and genesis are deleted here
		// - trie node
		// - legacy contract code
		// - new-scheme contract code
		isCode, codeKey := rawdb.IsCodeKey(key)
		if len(key) == common.HashLength || isCode {
			checkKey := key
			if isCode {
				checkKey = codeKey
			}
			if stateBloom.Contain(checkKey) {
				skipped += 1
				continue
			}
			count += 1
			size += common.StorageSize(len(key) + len(iter.Value()))
			batch.Delete(key)

			var eta time.Duration // Realistically will never remain uninited
			if done := binary.BigEndian.Uint64(key[:8]); done > 0 {
				var (
					left  = math.MaxUint64 - binary.BigEndian.Uint64(key[:8])
					speed = done/uint64(time.Since(pstart)/time.Millisecond+1) + 1 // +1s to avoid division by zero
				)
				eta = time.Duration(left/speed) * time.Millisecond
			}
			if time.Since(logged) > 8*time.Second {
				log.Info("Pruning state data", "nodes", count, "skipped", skipped, "size", size,
					"elapsed", common.PrettyDuration(time.Since(pstart)), "eta", common.PrettyDuration(eta))
				logged = time.Now()
			}
			// Recreate the iterator after every batch commit in order
			// to allow the underlying compactor to delete the entries.
			if batch.ValueSize() >= ethdb.IdealBatchSize {
				batch.Write()
				batch.Reset()

				iter.Release()
				iter = maindb.NewIterator(nil, key)
			}
		}
	}
	if batch.ValueSize() > 0 {
		batch.Write()
		batch.Reset()
	}
	iter.Release()
	log.Info("Pruned state data", "nodes", count, "size", size, "elapsed", common.PrettyDuration(time.Since(pstart)))

	var snapRoot common.Hash
	if len(allRoots) > 0 {
		snapRoot = allRoots[len(allRoots)-1]
	}
	if snapRoot != (common.Hash{}) && snaptree.Snapshot(snapRoot) != nil {
		// Pruning is done, now drop the "useless" layers from the snapshot.
		// Firstly, flushing the target layer into the disk. After that all
		// diff layers below the target will all be merged into the disk.
		if err := snaptree.Cap(snapRoot, 0); err != nil {
			return err
		}
		// Secondly, flushing the snapshot journal into the disk. All diff
		// layers upon are dropped silently. Eventually the entire snapshot
		// tree is converted into a single disk layer with the pruning target
		// as the root.
		if _, err := snaptree.Journal(snapRoot); err != nil {
			return err
		}
	}

	// Clean up any false positives that are top-level state roots.
	err := removeOtherRoots(maindb, allRoots, stateBloom, threads)
	if err != nil {
		return err
	}

	// Delete the state bloom, it marks the entire pruning procedure is
	// finished. If any crashes or manual exit happens before this,
	// `RecoverPruning` will pick it up in the next restarts to redo all
	// the things.
	os.RemoveAll(bloomPath)

	// Start compactions, will remove the deleted data from the disk immediately.
	// Note for small pruning, the compaction is skipped.
	if count >= rangeCompactionThreshold {
		cstart := time.Now()
		for b := 0x00; b <= 0xf0; b += 0x10 {
			var (
				start = []byte{byte(b)}
				end   = []byte{byte(b + 0x10)}
			)
			if b == 0xf0 {
				end = nil
			}
			log.Info("Compacting database", "range", fmt.Sprintf("%#x-%#x", start, end), "elapsed", common.PrettyDuration(time.Since(cstart)))
			if err := maindb.Compact(start, end); err != nil {
				log.Error("Database compaction failed", "error", err)
				return err
			}
		}
		log.Info("Database compaction finished", "elapsed", common.PrettyDuration(time.Since(cstart)))
	}
	log.Info("State pruning successful", "pruned", size, "elapsed", common.PrettyDuration(time.Since(start)))
	return nil
}

// We assume state blooms do not need the value, only the key
func dumpRawTrieDescendants(db ethdb.Database, root common.Hash, output *stateBloom, config *Config) error {
	// Offline pruning is only supported in legacy hash based scheme.
	hashConfig := *hashdb.Defaults
	hashConfig.CleanCacheSize = config.CleanCacheSize * 1024 * 1024
	trieConfig := &triedb.Config{
		Preimages: false,
		HashDB:    &hashConfig,
	}
	sdb := state.NewDatabaseWithConfig(db, trieConfig)
	defer sdb.TrieDB().Close()
	tr, err := sdb.OpenTrie(root)
	if err != nil {
		return err
	}
	accountIt, err := tr.NodeIterator(nil)
	if err != nil {
		return err
	}
	startedAt := time.Now()
	lastLog := time.Now()

	// We dump the storage of different accounts in parallel, but we want to limit this parallelism.
	// To do so, we create a semaphore out of a channel's buffer.
	// Before launching a new goroutine, we acquire the semaphore by taking an entry from this channel.
	// This channel doubles as a mechanism for the background goroutine to report an error on release.
	threads := config.Threads
	results := make(chan error, threads)
	for i := 0; i < threads; i++ {
		results <- nil
	}
	var threadsRunning atomic.Int32

	for accountIt.Next(true) {
		accountTrieHash := accountIt.Hash()
		// If the iterator hash is the empty hash, this is an embedded node
		if accountTrieHash != (common.Hash{}) {
			err = output.Put(accountTrieHash.Bytes(), nil)
			if err != nil {
				return err
			}
		}
		if accountIt.Leaf() {
			keyBytes := accountIt.LeafKey()
			if len(keyBytes) != len(common.Hash{}) {
				return fmt.Errorf("unexpected db key length %v", len(keyBytes))
			}
			key := common.BytesToHash(keyBytes)
			if time.Since(lastLog) >= time.Second*30 {
				lastLog = time.Now()
				progress := binary.BigEndian.Uint16(key.Bytes()[:2])
				elapsed := time.Since(startedAt)
				log.Info("traversing trie database", "key", key, "elapsed", elapsed, "eta", time.Duration(float32(elapsed)*(256*256/float32(progress)))-elapsed)
			}
			var data types.StateAccount
			if err := rlp.DecodeBytes(accountIt.LeafBlob(), &data); err != nil {
				return fmt.Errorf("failed to decode account data: %w", err)
			}
			if !bytes.Equal(data.CodeHash, types.EmptyCodeHash[:]) {
				output.Put(data.CodeHash, nil)
			}
			if data.Root != (common.Hash{}) {
				// note: we are passing data.Root as stateRoot here, to skip the check for stateRoot existence in trie.newTrieReader,
				// we already check that when opening state trie and reading the account node
				trieID := trie.StorageTrieID(data.Root, key, data.Root)
				storageTr, err := trie.NewStateTrie(trieID, sdb.TrieDB())
				if err != nil {
					return err
				}
				err = <-results
				if err != nil {
					return err
				}
				go func() {
					threadsRunning.Add(1)
					defer threadsRunning.Add(-1)
					var err error
					defer func() {
						results <- err
					}()
					threadStartedAt := time.Now()
					threadLastLog := time.Now()

					storageIt, err := storageTr.NodeIterator(nil)
					if err != nil {
						return
					}
					var processedNodes uint64
					for storageIt.Next(true) {
						storageTrieHash := storageIt.Hash()
						if storageTrieHash != (common.Hash{}) {
							// The inner bloomfilter library has a mutex so concurrency is fine here
							err = output.Put(storageTrieHash.Bytes(), nil)
							if err != nil {
								return
							}
						}
						processedNodes++
						if time.Since(threadLastLog) > 5*time.Minute {
							elapsedTotal := time.Since(startedAt)
							elapsedThread := time.Since(threadStartedAt)
							log.Info("traversing trie database - traversing storage trie taking long", "key", key, "elapsedTotal", elapsedTotal, "elapsedThread", elapsedThread, "processedNodes", processedNodes, "threadsRunning", threadsRunning.Load())
							threadLastLog = time.Now()
						}
					}
					err = storageIt.Error()
					if err != nil {
						return
					}
				}()
			}
		}
	}
	if accountIt.Error() != nil {
		return accountIt.Error()
	}
	for i := 0; i < threads; i++ {
		err = <-results
		if err != nil {
			return err
		}
	}
	return nil
}

// Prune deletes all historical state nodes except the nodes belong to the
// specified state version. If user doesn't specify the state version, use
// the bottom-most snapshot diff layer as the target.
func (p *Pruner) Prune(inputRoots []common.Hash) error {
	// If the state bloom filter is already committed previously,
	// reuse it for pruning instead of generating a new one. It's
	// mandatory because a part of state may already be deleted,
	// the recovery procedure is necessary.
	bloomExists, err := bloomFilterExists(p.config.Datadir)
	if err != nil {
		return err
	}
	if bloomExists {
		return RecoverPruning(p.config.Datadir, p.db, p.config.Threads)
	}
	// Retrieve all snapshot layers from the current HEAD.
	// In theory there are 128 difflayers + 1 disk layer present,
	// so 128 diff layers are expected to be returned.
	layers := p.snaptree.Snapshots(p.chainHeader.Root, -1, true)
	var roots []common.Hash // replaces zero roots with snapshot roots
	for _, root := range inputRoots {
		snapshotTarget := root == common.Hash{}
		if snapshotTarget {
			if len(layers) == 0 {
				log.Warn("No snapshot exists as pruning target")
				continue
			}
			// Use the bottom-most diff layer as the target
			root = layers[len(layers)-1].Root()
		}
		// Ensure the root is really present. The weak assumption
		// is the presence of root can indicate the presence of the
		// entire trie.
		if !rawdb.HasLegacyTrieNode(p.db, root) {
			if !snapshotTarget {
				return fmt.Errorf("associated state[%x] is not present", root)
			}
			// The special case is for clique based networks(rinkeby, goerli
			// and some other private networks), it's possible that two
			// consecutive blocks will have same root. In this case snapshot
			// difflayer won't be created. So HEAD-127 may not paired with
			// head-127 layer. Instead the paired layer is higher than the
			// bottom-most diff layer. Try to find the bottom-most snapshot
			// layer with state available.
			var found bool
			for i := len(layers) - 2; i >= 0; i-- {
				if rawdb.HasLegacyTrieNode(p.db, layers[i].Root()) {
					root = layers[i].Root()
					found = true
					log.Info("Selecting middle-layer as the pruning target", "root", root, "depth", i)
					break
				}
			}
			if !found {
				return errors.New("no snapshot paired state")
			}
		} else {
			if len(layers) > 0 {
				log.Info("Selecting bottom-most difflayer as the pruning target", "root", root, "height", p.chainHeader.Number.Uint64()-127)
			} else {
				log.Info("Selecting user-specified state as the pruning target", "root", root)
			}
		}
		roots = append(roots, root)
	}
	if len(roots) == 0 {
		return errors.New("no pruning target roots found")
	}

	// Traverse the target state, re-construct the whole state trie and
	// commit to the given bloom filter.
	start := time.Now()
	for _, root := range roots {
		log.Info("Building bloom filter for pruning", "root", root)
		if p.snaptree.Snapshot(root) != nil {
			if err := snapshot.GenerateTrie(p.snaptree, root, p.db, p.stateBloom); err != nil {
				return err
			}
		} else {
			if err := dumpRawTrieDescendants(p.db, root, p.stateBloom, &p.config); err != nil {
				return err
			}
		}
	}
	// Traverse the genesis, put all genesis state entries into the
	// bloom filter too.
	if err := extractGenesis(p.db, p.stateBloom, &p.config); err != nil {
		return err
	}

	filterName := bloomFilterPath(p.config.Datadir)

	log.Info("Writing state bloom to disk", "name", filterName, "roots", roots)
	if err := p.stateBloom.Commit(filterName, filterName+stateBloomFileTempSuffix, roots); err != nil {
		return err
	}
	log.Info("State bloom filter committed", "name", filterName, "roots", roots)
	return prune(p.snaptree, roots, p.db, p.stateBloom, filterName, start, p.config.Threads)
}

// RecoverPruning will resume the pruning procedure during the system restart.
// This function is used in this case: user tries to prune state data, but the
// system was interrupted midway because of crash or manual-kill. In this case
// if the bloom filter for filtering active state is already constructed, the
// pruning can be resumed. What's more if the bloom filter is constructed, the
// pruning **has to be resumed**. Otherwise a lot of dangling nodes may be left
// in the disk.
func RecoverPruning(datadir string, db ethdb.Database, threads int) error {
	exists, err := bloomFilterExists(datadir)
	if err != nil {
		return err
	}
	if !exists {
		return nil // nothing to recover
	}
	headBlock := rawdb.ReadHeadBlock(db)
	if headBlock == nil {
		return errors.New("failed to load head block")
	}
	// Initialize the snapshot tree in recovery mode to handle this special case:
	// - Users run the `prune-state` command multiple times
	// - Neither these `prune-state` running is finished(e.g. interrupted manually)
	// - The state bloom filter is already generated, a part of state is deleted,
	//   so that resuming the pruning here is mandatory
	// - The state HEAD is rewound already because of multiple incomplete `prune-state`
	// In this case, even the state HEAD is not exactly matched with snapshot, it
	// still feasible to recover the pruning correctly.
	snapconfig := snapshot.Config{
		CacheSize:  256,
		Recovery:   true,
		NoBuild:    true,
		AsyncBuild: false,
	}
	// Offline pruning is only supported in legacy hash based scheme.
	triedb := triedb.NewDatabase(db, triedb.HashDefaults)
	snaptree, err := snapshot.New(snapconfig, db, triedb, headBlock.Root())
	if err != nil {
		return err // The relevant snapshot(s) might not exist
	}
	stateBloomPath := bloomFilterPath(datadir)
	stateBloom, stateBloomRoots, err := NewStateBloomFromDisk(stateBloomPath)
	if err != nil {
		return err
	}
	log.Info("Loaded state bloom filter", "path", stateBloomPath, "roots", stateBloomRoots)

	return prune(snaptree, stateBloomRoots, db, stateBloom, stateBloomPath, time.Now(), threads)
}

// extractGenesis loads the genesis state and commits all the state entries
// into the given bloomfilter.
func extractGenesis(db ethdb.Database, stateBloom *stateBloom, config *Config) error {
	genesisHash := rawdb.ReadCanonicalHash(db, 0)
	if genesisHash == (common.Hash{}) {
		return errors.New("missing genesis hash")
	}
	genesis := rawdb.ReadBlock(db, genesisHash, 0)
	if genesis == nil {
		return errors.New("missing genesis block")
	}

	return dumpRawTrieDescendants(db, genesis.Root(), stateBloom, config)
}

func bloomFilterPath(datadir string) string {
	return filepath.Join(datadir, stateBloomFileName)
}

func bloomFilterExists(datadir string) (bool, error) {
	_, err := os.Stat(bloomFilterPath(datadir))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		return true, nil
	}
}
