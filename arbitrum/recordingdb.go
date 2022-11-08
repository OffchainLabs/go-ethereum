package arbitrum

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type RecordingKV struct {
	inner         *trie.Database
	readDbEntries map[common.Hash][]byte
	enableBypass  bool
}

func newRecordingKV(inner *trie.Database) *RecordingKV {
	return &RecordingKV{inner, make(map[common.Hash][]byte), false}
}

func (db *RecordingKV) Has(key []byte) (bool, error) {
	return false, errors.New("recording KV doesn't support Has")
}

func (db *RecordingKV) Get(key []byte) ([]byte, error) {
	var hash common.Hash
	var res []byte
	var err error
	if len(key) == 32 {
		copy(hash[:], key)
		res, err = db.inner.Node(hash)
	} else if len(key) == len(rawdb.CodePrefix)+32 && bytes.HasPrefix(key, rawdb.CodePrefix) {
		// Retrieving code
		copy(hash[:], key[len(rawdb.CodePrefix):])
		res, err = db.inner.DiskDB().Get(key)
	} else {
		err = fmt.Errorf("recording KV attempted to access non-hash key %v", hex.EncodeToString(key))
	}
	if err != nil {
		return nil, err
	}
	if db.enableBypass {
		return res, nil
	}
	if crypto.Keccak256Hash(res) != hash {
		return nil, fmt.Errorf("recording KV attempted to access non-hash key %v", hash)
	}
	db.readDbEntries[hash] = res
	return res, nil
}

func (db *RecordingKV) Put(key []byte, value []byte) error {
	return errors.New("recording KV doesn't support Put")
}

func (db *RecordingKV) Delete(key []byte) error {
	return errors.New("recording KV doesn't support Delete")
}

func (db *RecordingKV) NewBatch() ethdb.Batch {
	if db.enableBypass {
		return db.inner.DiskDB().NewBatch()
	}
	log.Error("recording KV: attempted to create batch when bypass not enabled")
	return nil
}

func (db *RecordingKV) NewBatchWithSize(size int) ethdb.Batch {
	if db.enableBypass {
		return db.inner.DiskDB().NewBatchWithSize(size)
	}
	log.Error("recording KV: attempted to create batch when bypass not enabled")
	return nil
}

func (db *RecordingKV) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	if db.enableBypass {
		return db.inner.DiskDB().NewIterator(prefix, start)
	}
	log.Error("recording KV: attempted to create iterator when bypass not enabled")
	return nil
}

func (db *RecordingKV) NewSnapshot() (ethdb.Snapshot, error) {
	// This is fine as RecordingKV doesn't support mutation
	return db, nil
}

func (db *RecordingKV) Stat(property string) (string, error) {
	return "", errors.New("recording KV doesn't support Stat")
}

func (db *RecordingKV) Compact(start []byte, limit []byte) error {
	return nil
}

func (db *RecordingKV) Close() error {
	return nil
}

func (db *RecordingKV) Release() {
	return
}

func (db *RecordingKV) GetRecordedEntries() map[common.Hash][]byte {
	return db.readDbEntries
}
func (db *RecordingKV) EnableBypass() {
	db.enableBypass = true
}

type RecordingChainContext struct {
	bc                     core.ChainContext
	minBlockNumberAccessed uint64
	initialBlockNumber     uint64
}

func newRecordingChainContext(inner core.ChainContext, blocknumber uint64) *RecordingChainContext {
	return &RecordingChainContext{
		bc:                     inner,
		minBlockNumberAccessed: blocknumber,
		initialBlockNumber:     blocknumber,
	}
}

func (r *RecordingChainContext) Engine() consensus.Engine {
	return r.bc.Engine()
}

func (r *RecordingChainContext) GetHeader(hash common.Hash, num uint64) *types.Header {
	if num < r.minBlockNumberAccessed {
		r.minBlockNumberAccessed = num
	}
	return r.bc.GetHeader(hash, num)
}

func (r *RecordingChainContext) GetMinBlockNumberAccessed() uint64 {
	return r.minBlockNumberAccessed
}

type RecordingDatabase struct {
	db         state.Database
	bc         *core.BlockChain
	mutex      sync.Mutex // protects StateFor and Dereference
	references int64
}

func NewRecordingDatabase(ethdb ethdb.Database, blockchain *core.BlockChain) *RecordingDatabase {
	return &RecordingDatabase{
		db: state.NewDatabaseWithConfig(ethdb, &trie.Config{Cache: 16}), //TODO cache needed? configurable?
		bc: blockchain,
	}
}

// Normal geth state.New + Reference is not atomic vs Dereference. This one is.
// This function does not recreate a state
func (r *RecordingDatabase) StateFor(header *types.Header) (*state.StateDB, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	sdb, err := state.NewDeterministic(header.Root, r.db)
	if err == nil {
		r.referenceRootLockHeld(header.Root)
	}
	return sdb, err
}

func (r *RecordingDatabase) Dereference(header *types.Header) {
	if header != nil {
		r.dereferenceRoot(header.Root)
	}
}

func (r *RecordingDatabase) WriteStateToDatabase(header *types.Header) error {
	if header != nil {
		return r.db.TrieDB().Commit(header.Root, true, nil)
	}
	return nil
}

// lock must be held when calling that
func (r *RecordingDatabase) referenceRootLockHeld(root common.Hash) {
	r.references++
	r.db.TrieDB().Reference(root, common.Hash{})
}

func (r *RecordingDatabase) dereferenceRoot(root common.Hash) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.references--
	r.db.TrieDB().Dereference(root)
}

func (r *RecordingDatabase) addStateVerify(statedb *state.StateDB, expected common.Hash) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	result, err := statedb.Commit(true)
	if err != nil {
		return err
	}
	if result != expected {
		return fmt.Errorf("bad root hash expected: %v got: %v", expected, result)
	}
	r.referenceRootLockHeld(result)
	return nil
}

type StateBuildingLogFunction func(targetHeader, header *types.Header, hasState bool)

func (r *RecordingDatabase) PrepareRecording(ctx context.Context, lastBlockHeader *types.Header, logFunc StateBuildingLogFunction) (*state.StateDB, core.ChainContext, *RecordingKV, error) {
	_, err := r.GetOrRecreateState(ctx, lastBlockHeader, logFunc)
	if err != nil {
		return nil, nil, nil, err
	}
	finalDereference := lastBlockHeader // dereference in case of error
	defer func() { r.Dereference(finalDereference) }()
	recordingKeyValue := newRecordingKV(r.db.TrieDB())

	recordingStateDatabase := state.NewDatabase(rawdb.NewDatabase(recordingKeyValue))
	var prevRoot common.Hash
	if lastBlockHeader != nil {
		prevRoot = lastBlockHeader.Root
	}
	recordingStateDb, err := state.NewDeterministic(prevRoot, recordingStateDatabase)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create recordingStateDb: %w", err)
	}
	var recordingChainContext *RecordingChainContext
	if lastBlockHeader != nil {
		if !lastBlockHeader.Number.IsUint64() {
			return nil, nil, nil, errors.New("block number not uint64")
		}
		recordingChainContext = newRecordingChainContext(r.bc, lastBlockHeader.Number.Uint64())
	}
	finalDereference = nil
	return recordingStateDb, recordingChainContext, recordingKeyValue, nil
}

func (r *RecordingDatabase) PreimagesFromRecording(chainContextIf core.ChainContext, recordingDb *RecordingKV) (map[common.Hash][]byte, error) {
	entries := recordingDb.GetRecordedEntries()
	recordingChainContext, ok := chainContextIf.(*RecordingChainContext)
	if (recordingChainContext == nil) || (!ok) {
		return nil, errors.New("recordingChainContext invalid")
	}

	for i := recordingChainContext.GetMinBlockNumberAccessed(); i <= recordingChainContext.initialBlockNumber; i++ {
		header := r.bc.GetHeaderByNumber(i)
		hash := header.Hash()
		bytes, err := rlp.EncodeToBytes(header)
		if err != nil {
			return nil, fmt.Errorf("Error RLP encoding header: %v\n", err)
		}
		entries[hash] = bytes
	}
	return entries, nil
}

func (r *RecordingDatabase) GetOrRecreateState(ctx context.Context, header *types.Header, logFunc StateBuildingLogFunction) (*state.StateDB, error) {
	stateDb, err := r.StateFor(header)
	if err == nil {
		return stateDb, nil
	}
	returnedBlockNumber := header.Number.Uint64()
	genesis := r.bc.Config().ArbitrumChainParams.GenesisBlockNum
	currentHeader := header
	var lastRoot common.Hash
	for ctx.Err() == nil {
		if logFunc != nil {
			logFunc(header, currentHeader, false)
		}
		if currentHeader.Number.Uint64() <= genesis {
			return nil, fmt.Errorf("moved beyond genesis looking for state looking for %d, genesis %d, err %w", returnedBlockNumber, genesis, err)
		}
		currentHeader = r.bc.GetHeader(currentHeader.ParentHash, currentHeader.Number.Uint64()-1)
		if currentHeader == nil {
			return nil, fmt.Errorf("chain doesn't contain parent of block %d hash %v", currentHeader.Number, currentHeader.Hash())
		}
		stateDb, err = r.StateFor(currentHeader)
		if err == nil {
			lastRoot = currentHeader.Root
			break
		}
	}
	defer func() {
		if (lastRoot != common.Hash{}) {
			r.dereferenceRoot(lastRoot)
		}
	}()
	blockToRecreate := currentHeader.Number.Uint64() + 1
	prevHash := currentHeader.Hash()
	for ctx.Err() == nil {
		block := r.bc.GetBlockByNumber(blockToRecreate)
		if block == nil {
			return nil, fmt.Errorf("block not found while recreating: %d", blockToRecreate)
		}
		if block.ParentHash() != prevHash {
			return nil, fmt.Errorf("reorg detected: number %d expectedPrev: %v foundPrev: %v", blockToRecreate, prevHash, block.ParentHash())
		}
		prevHash = block.Hash()
		if logFunc != nil {
			logFunc(header, block.Header(), true)
		}
		_, _, _, err := r.bc.Processor().Process(block, stateDb, vm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed recreating state for block %d : %w", blockToRecreate, err)
		}
		err = r.addStateVerify(stateDb, block.Root())
		if err != nil {
			return nil, fmt.Errorf("failed commiting state for block %d : %w", blockToRecreate, err)
		}
		r.dereferenceRoot(lastRoot)
		lastRoot = block.Root()
		if blockToRecreate >= returnedBlockNumber {
			if block.Hash() != header.Hash() {
				return nil, fmt.Errorf("blockHash doesn't match when recreating number: %d expected: %v got: %v", blockToRecreate, header.Hash(), block.Hash())
			}
			// don't dereference this one
			lastRoot = common.Hash{}
			return stateDb, nil
		}
		blockToRecreate++
	}
	return nil, ctx.Err()
}

func (r *RecordingDatabase) ReferenceCount() int64 {
	return r.references
}
