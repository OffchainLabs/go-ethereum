package arbitrum

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

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

func NewRecordingKV(inner *trie.Database) *RecordingKV {
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

func NewRecordingChainContext(inner core.ChainContext, blocknumber uint64) *RecordingChainContext {
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

func PrepareRecording(trieDB *trie.Database, chainContext core.ChainContext, lastBlockHeader *types.Header) (*state.StateDB, core.ChainContext, *RecordingKV, error) {
	recordingKeyValue := NewRecordingKV(trieDB)

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
		recordingChainContext = NewRecordingChainContext(chainContext, lastBlockHeader.Number.Uint64())
	}
	return recordingStateDb, recordingChainContext, recordingKeyValue, nil
}

func PreimagesFromRecording(chainContextIf core.ChainContext, recordingDb *RecordingKV) (map[common.Hash][]byte, error) {
	entries := recordingDb.GetRecordedEntries()
	recordingChainContext, ok := chainContextIf.(*RecordingChainContext)
	if (recordingChainContext == nil) || (!ok) {
		return nil, errors.New("recordingChainContext invalid")
	}
	blockchain, ok := recordingChainContext.bc.(*core.BlockChain)
	if (blockchain == nil) || (!ok) {
		return nil, errors.New("blockchain invalid")
	}
	for i := recordingChainContext.GetMinBlockNumberAccessed(); i <= recordingChainContext.initialBlockNumber; i++ {
		header := blockchain.GetHeaderByNumber(i)
		hash := header.Hash()
		bytes, err := rlp.EncodeToBytes(header)
		if err != nil {
			panic(fmt.Sprintf("Error RLP encoding header: %v\n", err))
		}
		entries[hash] = bytes
	}
	return entries, nil
}

func GetOrRecreateReferencedState(ctx context.Context, header *types.Header, bc *core.BlockChain, stateDatabase state.Database) (*state.StateDB, error) {
	trieDB := stateDatabase.TrieDB()
	stateDb, err := state.New(header.Root, stateDatabase, nil)
	if err == nil {
		trieDB.Reference(header.Root, common.Hash{})
		return stateDb, nil
	}
	returnedBlockNumber := header.Number.Uint64()
	genesis := bc.Config().ArbitrumChainParams.GenesisBlockNum
	currentHeader := header
	var lastRoot common.Hash
	for ctx.Err() == nil {
		if currentHeader.Number.Uint64() <= genesis {
			return nil, fmt.Errorf("moved beyond genesis looking for state looking for %d, genesis %d, err %w", returnedBlockNumber, genesis, err)
		}
		currentHeader = bc.GetHeader(currentHeader.ParentHash, currentHeader.Number.Uint64()-1)
		if currentHeader == nil {
			return nil, fmt.Errorf("chain doesn't contain parent of block %d hash %v", currentHeader.Number, currentHeader.Hash())
		}
		stateDb, err = state.New(currentHeader.Root, stateDatabase, nil)
		if err == nil {
			trieDB.Reference(header.Root, common.Hash{})
			lastRoot = currentHeader.Root
			defer func() { trieDB.Dereference(lastRoot) }()
			break
		}
	}
	blockToRecreate := currentHeader.Number.Uint64() + 1
	for ctx.Err() == nil {
		block := bc.GetBlockByNumber(blockToRecreate)
		if block == nil {
			return nil, fmt.Errorf("block not found while recreating: %d", blockToRecreate)
		}
		_, _, _, err := bc.Processor().Process(block, stateDb, vm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed recreating state for block %d : %w", blockToRecreate, err)
		}
		root, err := stateDb.Commit(true)
		if err != nil {
			return nil, fmt.Errorf("failed commiting state for block %d : %w", blockToRecreate, err)
		}
		if root != block.Root() {
			return nil, fmt.Errorf("bad state recreating block %d : exp: %v got %v", blockToRecreate, block.Root(), root)
		}
		trieDB.Reference(block.Root(), common.Hash{})
		lastRoot = block.Root()
		if blockToRecreate >= returnedBlockNumber {
			if block.Hash() != header.Hash() {
				return nil, fmt.Errorf("blockHash doesn't match when recreating number: %d expected: %v got: %v", blockToRecreate, header.Hash(), block.Hash())
			}
			// double reference because te defer is going to remove one
			trieDB.Reference(block.Root(), common.Hash{})
			return stateDb, nil
		}
		blockToRecreate++
	}
	return nil, ctx.Err()
}

func ReferenceState(header *types.Header, stateDatabase state.Database) {
	stateDatabase.TrieDB().Reference(header.Root, common.Hash{})
}

func DereferenceState(header *types.Header, stateDatabase state.Database) {
	stateDatabase.TrieDB().Dereference(header.Root)
}
