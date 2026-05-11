// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package arbitrum

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

type recordingNodeReader struct {
	inner           database.NodeReader
	recordingTrieDB *RecordingTrieDB
}

func (r *recordingNodeReader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	preimage, err := r.inner.Node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if preimage != nil {
		r.recordingTrieDB.record(hash, preimage)
	}
	return preimage, nil
}

type RecordingTrieDB struct {
	*triedb.Database
	mutex      sync.Mutex
	preimages  map[common.Hash][]byte
	codeHashes map[common.Hash]struct{}
}

func NewRecordingTrieDB(inner *triedb.Database) *RecordingTrieDB {
	return &RecordingTrieDB{
		Database:   inner,
		preimages:  make(map[common.Hash][]byte),
		codeHashes: make(map[common.Hash]struct{}),
	}
}

func (d *RecordingTrieDB) NodeReader(stateRoot common.Hash) (database.NodeReader, error) {
	reader, err := d.Database.NodeReader(stateRoot)
	if err != nil {
		return nil, err
	}
	return &recordingNodeReader{
		inner:           reader,
		recordingTrieDB: d,
	}, nil
}

func (d *RecordingTrieDB) record(hash common.Hash, blob []byte) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if _, exists := d.preimages[hash]; exists {
		return
	}

	d.preimages[hash] = blob
}

func (d *RecordingTrieDB) recordCode(codeHash common.Hash) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.codeHashes[codeHash] = struct{}{}
}

func (d *RecordingTrieDB) Preimages() map[common.Hash][]byte {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	copied := make(map[common.Hash][]byte, len(d.preimages))
	for k, v := range d.preimages {
		copied[k] = v
	}
	return copied
}

func (d *RecordingTrieDB) CodeHashes() []common.Hash {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	codeHashes := make([]common.Hash, 0, len(d.codeHashes))
	for codeHash := range d.codeHashes {
		codeHashes = append(codeHashes, codeHash)
	}
	return codeHashes
}

type TipRecordingStateDatabase struct {
	state.Database
	recordingTrieDB *RecordingTrieDB
}

func NewTipRecordingStateDatabase(inner state.Database) *TipRecordingStateDatabase {
	return &TipRecordingStateDatabase{
		Database:        inner,
		recordingTrieDB: NewRecordingTrieDB(inner.TrieDB()),
	}
}

func (r *TipRecordingStateDatabase) Preimages() map[common.Hash][]byte {
	return r.recordingTrieDB.Preimages()
}

func (r *TipRecordingStateDatabase) CodeHashes() []common.Hash {
	return r.recordingTrieDB.CodeHashes()
}

func (r *TipRecordingStateDatabase) TrieDB() *triedb.Database {
	return r.recordingTrieDB.Database
}

func (r *TipRecordingStateDatabase) OpenTrie(root common.Hash) (state.Trie, error) {
	tr, err := trie.NewStateTrie(trie.StateTrieID(root), r.recordingTrieDB)
	if err != nil {
		return nil, err
	}
	return tr, nil
}

func (r *TipRecordingStateDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, parent state.Trie) (state.Trie, error) {
	tr, err := trie.NewStateTrie(trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(address.Bytes()), root), r.recordingTrieDB)
	if err != nil {
		return nil, err
	}
	return tr, nil
}

func (r *TipRecordingStateDatabase) Reader(root common.Hash) (state.Reader, error) {
	codeReader, err := r.Database.Reader(root)
	if err != nil {
		return nil, err
	}

	stateReader, err := state.NewTrieReader(root, r.recordingTrieDB, r.Database.PointCache())
	if err != nil {
		return nil, err
	}

	return &recordingReader{
		StateReader: stateReader,
		db:          r,
		codeReader:  codeReader,
	}, nil
}

type recordingReader struct {
	state.StateReader

	db         *TipRecordingStateDatabase
	codeReader state.ContractCodeReader // for code reads (we intercept and record)
}

func (r *recordingReader) Code(addr common.Address, codeHash common.Hash) ([]byte, error) {
	code, err := r.codeReader.Code(addr, codeHash)
	if err != nil {
		return nil, err
	}
	if len(code) > 0 {
		r.db.recordingTrieDB.recordCode(codeHash)
	}
	return code, nil
}

func (r *recordingReader) CodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	size, err := r.codeReader.CodeSize(addr, codeHash)
	if err != nil {
		return 0, err
	}
	if size > 0 {
		r.db.recordingTrieDB.recordCode(codeHash)
	}
	return size, nil
}
