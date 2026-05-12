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
	mutex     sync.Mutex
	stopped   bool
	preimages map[common.Hash][]byte
}

func NewRecordingTrieDB(inner *triedb.Database) *RecordingTrieDB {
	return &RecordingTrieDB{
		Database:  inner,
		preimages: make(map[common.Hash][]byte),
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

	if d.stopped {
		return
	}
	if _, exists := d.preimages[hash]; exists {
		return
	}

	d.preimages[hash] = blob
}

// StopRecording freezes the current recording so later trie reads are ignored.
func (d *RecordingTrieDB) StopRecording() {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.stopped = true
}

// Preimages returns the recorded trie preimages. StopRecording must be called
// first; the returned map is not safe while recording is active.
func (d *RecordingTrieDB) Preimages() map[common.Hash][]byte {
	return d.preimages
}

type TipRecordingStateDatabase struct {
	state.Database
	mutex           sync.Mutex
	stopped         bool
	codeHashes      map[common.Hash]struct{}
	recordingTrieDB *RecordingTrieDB
}

func NewTipRecordingStateDatabase(inner state.Database) *TipRecordingStateDatabase {
	return &TipRecordingStateDatabase{
		Database:        inner,
		codeHashes:      make(map[common.Hash]struct{}),
		recordingTrieDB: NewRecordingTrieDB(inner.TrieDB()),
	}
}

func (r *TipRecordingStateDatabase) Preimages() map[common.Hash][]byte {
	return r.recordingTrieDB.Preimages()
}

// StopRecording freezes the current tip recording.
func (r *TipRecordingStateDatabase) StopRecording() {
	r.mutex.Lock()
	r.stopped = true
	r.mutex.Unlock()

	r.recordingTrieDB.StopRecording()
}

func (r *TipRecordingStateDatabase) recordCode(codeHash common.Hash) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.stopped {
		return
	}
	r.codeHashes[codeHash] = struct{}{}
}

// CodeHashes returns the recorded code hashes. StopRecording must be called
// first; the returned slice is not safe while recording is active.
func (r *TipRecordingStateDatabase) CodeHashes() []common.Hash {
	codeHashes := make([]common.Hash, 0, len(r.codeHashes))
	for codeHash := range r.codeHashes {
		codeHashes = append(codeHashes, codeHash)
	}
	return codeHashes
}

func (r *TipRecordingStateDatabase) TrieDB() *triedb.Database {
	return r.recordingTrieDB.Database
}

func (r *TipRecordingStateDatabase) OpenTrie(root common.Hash) (state.Trie, error) {
	if r.recordingTrieDB.IsVerkle() {
		panic("TipRecordingStateDatabase.OpenTrie called with Verkle trie database")
	}
	tr, err := trie.NewStateTrie(trie.StateTrieID(root), r.recordingTrieDB)
	if err != nil {
		return nil, err
	}
	return tr, nil
}

func (r *TipRecordingStateDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, parent state.Trie) (state.Trie, error) {
	if r.recordingTrieDB.IsVerkle() {
		panic("TipRecordingStateDatabase.OpenStorageTrie called with Verkle trie database")
	}
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
		r.db.recordCode(codeHash)
	}
	return code, nil
}

func (r *recordingReader) CodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	size, err := r.codeReader.CodeSize(addr, codeHash)
	if err != nil {
		return 0, err
	}
	if size > 0 {
		r.db.recordCode(codeHash)
	}
	return size, nil
}
