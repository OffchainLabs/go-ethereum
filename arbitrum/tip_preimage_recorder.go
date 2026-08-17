// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package arbitrum

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/overlay"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/bintrie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

var (
	_ state.TrieReaderDatabase = (*RecordingTrieDB)(nil)
	_ state.Database           = (*TipRecordingStateDatabase)(nil)
	_ state.Reader             = (*recordingReader)(nil)
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

// Preimages returns a shallow copy of the recorded trie preimages.
// StopRecording should be called first for the returned snapshot to be
// complete.
func (d *RecordingTrieDB) Preimages() map[common.Hash][]byte {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	preimages := make(map[common.Hash][]byte, len(d.preimages))
	for hash, preimage := range d.preimages {
		preimages[hash] = preimage
	}
	return preimages
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

// CodeHashes returns the recorded code hashes. StopRecording should be called
// first for the returned snapshot to be complete.
func (r *TipRecordingStateDatabase) CodeHashes() []common.Hash {
	r.mutex.Lock()
	defer r.mutex.Unlock()

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
	if r.recordingTrieDB.IsUBT() {
		ts := overlay.LoadTransitionState(r.recordingTrieDB.Disk(), root, true)
		if ts.InTransition() {
			panic("state tree transition isn't supported yet")
		}
		if ts.Transitioned() {
			return bintrie.NewBinaryTrie(root, r.recordingTrieDB, r.recordingTrieDB.BinTrieGroupDepth())
		}
	}
	return trie.NewStateTrie(trie.StateTrieID(root), r.recordingTrieDB)
}

func (r *TipRecordingStateDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, parent state.Trie) (state.Trie, error) {
	if r.recordingTrieDB.IsUBT() {
		return parent, nil
	}
	return trie.NewStateTrie(trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(address.Bytes()), root), r.recordingTrieDB)
}

func (r *TipRecordingStateDatabase) Reader(root common.Hash) (state.Reader, error) {
	codeReader, err := r.Database.Reader(root)
	if err != nil {
		return nil, err
	}

	var stateReader state.StateReader
	if r.recordingTrieDB.IsUBT() {
		stateReader, err = state.NewUBTTrieReader(root, r.recordingTrieDB)
	} else {
		stateReader, err = state.NewMPTTrieReader(root, r.recordingTrieDB)
	}
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

func (r *recordingReader) Has(addr common.Address, codeHash common.Hash) bool {
	has := r.codeReader.Has(addr, codeHash)
	if has {
		r.db.recordCode(codeHash)
	}
	return has
}

func (r *recordingReader) Code(addr common.Address, codeHash common.Hash) []byte {
	code := r.codeReader.Code(addr, codeHash)
	if len(code) > 0 {
		r.db.recordCode(codeHash)
	}
	return code
}

func (r *recordingReader) CodeSize(addr common.Address, codeHash common.Hash) int {
	size := r.codeReader.CodeSize(addr, codeHash)
	if size > 0 {
		r.db.recordCode(codeHash)
	}
	return size
}
