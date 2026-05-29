// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package arbitrum

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/database"
)

type recordingNodeReader struct {
	stateRoot       common.Hash
	inner           database.NodeReader
	recordingTrieDB *RecordingTrieDB
}

func (r *recordingNodeReader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	preimage, err := r.inner.Node(owner, path, hash)
	if err != nil {
		return nil, err
	}
	if preimage != nil {
		r.recordingTrieDB.recordTrieNode(r.stateRoot, owner, path, hash, preimage)
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
		stateRoot:       stateRoot,
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

func (d *RecordingTrieDB) recorded(hash common.Hash) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	_, exists := d.preimages[hash]
	return exists
}

func (d *RecordingTrieDB) recordTrieNode(stateRoot common.Hash, owner common.Hash, path []byte, hash common.Hash, blob []byte) {
	d.record(hash, blob)
	reader, err := d.Database.NodeReader(stateRoot)
	if err != nil {
		return
	}
	gatherTrieHashChildren(path, blob, func(childPath []byte, child common.Hash) {
		if d.recorded(child) {
			return
		}
		childBlob, err := reader.Node(owner, childPath, child)
		if err != nil || len(childBlob) == 0 {
			return
		}
		d.record(child, childBlob)
	})
}

func (d *RecordingTrieDB) recordNodeSet(tr *recordingTrie, nodes *trienode.NodeSet) {
	if nodes == nil {
		return
	}
	for path, node := range nodes.Nodes {
		if node == nil || node.Hash == (common.Hash{}) || len(node.Blob) == 0 {
			continue
		}
		d.recordTrieNode(tr.stateRoot, tr.owner, []byte(path), node.Hash, node.Blob)
	}
	for path, origin := range nodes.Origins {
		if len(origin) == 0 {
			continue
		}
		d.recordTrieNode(tr.stateRoot, tr.owner, []byte(path), crypto.Keccak256Hash(origin), origin)
	}
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

func gatherTrieHashChildren(path []byte, blob []byte, onChild func([]byte, common.Hash)) {
	content, rest, err := rlp.SplitList(blob)
	if err != nil || len(rest) != 0 {
		return
	}
	count, err := rlp.CountValues(content)
	if err != nil {
		return
	}
	switch count {
	case 2:
		key, value, err := rlp.SplitString(content)
		if err != nil {
			return
		}
		kind, childContent, childRest, err := rlp.Split(value)
		if err != nil || len(childRest) != 0 {
			return
		}
		childPath := append(append([]byte(nil), path...), compactToHex(key)...)
		if len(childPath) > 0 && childPath[len(childPath)-1] == 16 {
			return
		}
		gatherTrieHashChild(childPath, kind, childContent, value, onChild)
	case 17:
		for i := 0; i < 16; i++ {
			kind, childContent, childRest, err := rlp.Split(content)
			if err != nil {
				return
			}
			encoded := content[:len(content)-len(childRest)]
			childPath := append(append([]byte(nil), path...), byte(i))
			gatherTrieHashChild(childPath, kind, childContent, encoded, onChild)
			content = childRest
		}
	}
}

func gatherTrieHashChild(path []byte, kind rlp.Kind, content []byte, encoded []byte, onChild func([]byte, common.Hash)) {
	switch {
	case kind == rlp.List:
		gatherTrieHashChildren(path, encoded, onChild)
	case len(content) == common.HashLength:
		onChild(path, common.BytesToHash(content))
	}
}

func compactToHex(compact []byte) []byte {
	if len(compact) == 0 {
		return compact
	}
	base := trie.KeybytesToHex(compact)
	if base[0] < 2 {
		base = base[:len(base)-1]
	}
	chop := 2 - base[0]&1
	return base[chop:]
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
	return &recordingTrie{Trie: tr, db: r.recordingTrieDB, stateRoot: root}, nil
}

func (r *TipRecordingStateDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, parent state.Trie) (state.Trie, error) {
	if r.recordingTrieDB.IsVerkle() {
		panic("TipRecordingStateDatabase.OpenStorageTrie called with Verkle trie database")
	}
	tr, err := trie.NewStateTrie(trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(address.Bytes()), root), r.recordingTrieDB)
	if err != nil {
		return nil, err
	}
	return &recordingTrie{Trie: tr, db: r.recordingTrieDB, stateRoot: stateRoot, owner: crypto.Keccak256Hash(address.Bytes())}, nil
}

type recordingTrie struct {
	state.Trie
	db        *RecordingTrieDB
	stateRoot common.Hash
	owner     common.Hash
}

func (t *recordingTrie) Commit(collectLeaf bool) (common.Hash, *trienode.NodeSet) {
	root, nodes := t.Trie.Commit(collectLeaf)
	t.db.recordNodeSet(t, nodes)
	return root, nodes
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
