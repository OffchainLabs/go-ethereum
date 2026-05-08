// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package arbitrum

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
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

	copied := make([]byte, len(blob))
	copy(copied, blob)
	d.preimages[hash] = copied
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
		copied[k] = common.CopyBytes(v)
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
	innerReader, err := r.Database.Reader(root)
	if err != nil {
		return nil, err
	}

	trie, err := r.OpenTrie(root)
	if err != nil {
		return nil, err
	}

	return &recordingReader{
		root:        root,
		db:          r,
		innerReader: innerReader,
		mainTrie:    trie,
		subRoots:    make(map[common.Address]common.Hash),
		subTries:    make(map[common.Address]state.Trie),
	}, nil
}

type recordingReader struct {
	root        common.Hash
	db          *TipRecordingStateDatabase
	innerReader state.Reader // for code reads (we intercept and record)

	// Trie state, mirroring trieReader in core/state/reader.go
	mainTrie state.Trie
	subRoots map[common.Address]common.Hash // Set of storage roots, cached when the account is resolved
	subTries map[common.Address]state.Trie  // Group of storage tries, cached when it's resolved
	lock     sync.Mutex                     // Lock for protecting concurrent read
}

// account is the inner version of Account and assumes the r.lock is already held.
func (r *recordingReader) account(addr common.Address) (*types.StateAccount, error) {
	account, err := r.mainTrie.GetAccount(addr)
	if err != nil {
		return nil, err
	}
	if account == nil {
		r.subRoots[addr] = types.EmptyRootHash
	} else {
		r.subRoots[addr] = account.Root
	}
	return account, nil
}

func (r *recordingReader) Account(addr common.Address) (*types.StateAccount, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	return r.account(addr)
}

func (r *recordingReader) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	tr, found := r.subTries[addr]
	if !found {
		root, ok := r.subRoots[addr]
		if !ok {
			_, err := r.account(addr)
			if err != nil {
				return common.Hash{}, err
			}
			root = r.subRoots[addr]
		}
		var err error
		tr, err = r.db.OpenStorageTrie(r.root, addr, root, nil)
		if err != nil {
			return common.Hash{}, err
		}
		r.subTries[addr] = tr
	}
	ret, err := tr.GetStorage(addr, slot.Bytes())
	if err != nil {
		return common.Hash{}, err
	}
	var value common.Hash
	value.SetBytes(ret)
	return value, nil
}

func (r *recordingReader) Code(addr common.Address, codeHash common.Hash) ([]byte, error) {
	code, err := r.innerReader.Code(addr, codeHash)
	if err != nil {
		return nil, err
	}
	if len(code) > 0 {
		r.db.recordingTrieDB.recordCode(codeHash)
	}
	return code, nil
}

func (r *recordingReader) CodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	size, err := r.innerReader.CodeSize(addr, codeHash)
	if err != nil {
		return 0, err
	}
	if size > 0 {
		r.db.recordingTrieDB.recordCode(codeHash)
	}
	return size, nil
}
