package arbitrum

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
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

func PrepareRecording(blockchain *core.BlockChain, lastBlockHeader *types.Header) (*state.StateDB, core.ChainContext, *RecordingKV, error) {
	rawTrie := blockchain.StateCache().TrieDB()
	recordingKeyValue := NewRecordingKV(rawTrie)
	recordingStateDatabase := state.NewDatabase(rawdb.NewDatabase(recordingKeyValue))
	var prevRoot common.Hash
	if lastBlockHeader != nil {
		prevRoot = lastBlockHeader.Root
	}
	recordingStateDb, err := state.New(prevRoot, recordingStateDatabase, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create recordingStateDb: %w", err)
	}
	var recordingChainContext *RecordingChainContext
	if lastBlockHeader != nil {
		if !lastBlockHeader.Number.IsUint64() {
			return nil, nil, nil, errors.New("block number not uint64")
		}
		recordingChainContext = NewRecordingChainContext(blockchain, lastBlockHeader.Number.Uint64())
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
