// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbitrum

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/ethereum/go-ethereum/triedb"
)

// RecordingStateDatabase wraps a state.Database and intercepts OpenTrie,
// OpenStorageTrie, and Reader to route all trie node reads through a
// RecordingTrieDB. This ensures that all trie nodes and contract code
// accessed during block execution are captured as preimages.
//
// Crucially, the Reader() override forces all account and storage reads
// through the trie (bypassing the flat/snapshot layer) so that trie nodes
// are captured. Without this, PathDB's flat reader would serve data without
// touching trie nodes, leaving the validator without the preimages it needs.
//
// All other methods (TrieDB, DiskDB, WasmStore, ActivatedAsm, Snapshot,
// PointCache) delegate to the underlying real database so that state commits
// and other operations work normally.
type RecordingStateDatabase struct {
	inner           state.Database
	recordingTrieDB *RecordingTrieDB
}

// NewRecordingStateDatabase creates a RecordingStateDatabase wrapping the given
// state database. The recordingTrieDB intercepts all trie node reads.
func NewRecordingStateDatabase(inner state.Database) *RecordingStateDatabase {
	return &RecordingStateDatabase{
		inner:           inner,
		recordingTrieDB: NewRecordingTrieDB(inner.TrieDB()),
	}
}

// OpenTrie opens the main account trie with recording enabled.
func (db *RecordingStateDatabase) OpenTrie(root common.Hash) (state.Trie, error) {
	tr, err := trie.NewStateTrie(trie.StateTrieID(root), db.recordingTrieDB)
	if err != nil {
		return nil, err
	}
	return tr, nil
}

// OpenStorageTrie opens the storage trie of an account with recording enabled.
func (db *RecordingStateDatabase) OpenStorageTrie(stateRoot common.Hash, address common.Address, root common.Hash, self state.Trie) (state.Trie, error) {
	tr, err := trie.NewStateTrie(
		trie.StorageTrieID(stateRoot, crypto.Keccak256Hash(address.Bytes()), root),
		db.recordingTrieDB,
	)
	if err != nil {
		return nil, err
	}
	return tr, nil
}

// Preimages returns all captured trie node and code preimages.
func (db *RecordingStateDatabase) Preimages() map[common.Hash][]byte {
	return db.recordingTrieDB.Preimages()
}

// Reader returns a state reader that forces all reads through the recording
// trie DB (bypassing the flat/snapshot layer) and captures contract code.
// This ensures all trie nodes needed by the validator are recorded.
func (db *RecordingStateDatabase) Reader(root common.Hash) (state.Reader, error) {
	// Get the inner reader for code access (we'll wrap it to record code reads)
	innerReader, err := db.inner.Reader(root)
	if err != nil {
		return nil, err
	}
	return &recordingReader{
		root:        root,
		db:          db,
		innerReader: innerReader,
		subRoots:    make(map[common.Address]common.Hash),
		subTries:    make(map[common.Address]state.Trie),
	}, nil
}

// --- Delegate all other state.Database methods to the inner database ---

func (db *RecordingStateDatabase) DiskDB() ethdb.KeyValueStore {
	return db.inner.DiskDB()
}

func (db *RecordingStateDatabase) TrieDB() *triedb.Database {
	return db.inner.TrieDB()
}

func (db *RecordingStateDatabase) Snapshot() *snapshot.Tree {
	return db.inner.Snapshot()
}

func (db *RecordingStateDatabase) PointCache() *utils.PointCache {
	return db.inner.PointCache()
}

func (db *RecordingStateDatabase) ActivatedAsm(target rawdb.WasmTarget, moduleHash common.Hash) []byte {
	return db.inner.ActivatedAsm(target, moduleHash)
}

func (db *RecordingStateDatabase) WasmStore() ethdb.KeyValueStore {
	return db.inner.WasmStore()
}

// recordingReader implements state.Reader (ContractCodeReader + StateReader).
// It uses the recording trie DB for account and storage reads (bypassing the
// flat/snapshot layer) and records contract code reads as preimages.
type recordingReader struct {
	root        common.Hash
	db          *RecordingStateDatabase
	innerReader state.Reader // for code reads (we intercept and record)

	// Trie state, mirroring trieReader in core/state/reader.go
	mainTrie state.Trie
	subRoots map[common.Address]common.Hash
	subTries map[common.Address]state.Trie
	lock     sync.Mutex
}

// ensureMainTrie lazily opens the main account trie.
func (r *recordingReader) ensureMainTrie() error {
	if r.mainTrie != nil {
		return nil
	}
	tr, err := r.db.OpenTrie(r.root)
	if err != nil {
		return err
	}
	r.mainTrie = tr
	return nil
}

// Account implements StateReader. It reads the account from the recording trie,
// ensuring all trie nodes on the path are captured.
func (r *recordingReader) Account(addr common.Address) (*types.StateAccount, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if err := r.ensureMainTrie(); err != nil {
		return nil, err
	}
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

// Storage implements StateReader. It reads the storage value from the recording
// trie, ensuring all trie nodes on the path are captured.
func (r *recordingReader) Storage(addr common.Address, slot common.Hash) (common.Hash, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if err := r.ensureMainTrie(); err != nil {
		return common.Hash{}, err
	}

	tr, found := r.subTries[addr]
	if !found {
		root, ok := r.subRoots[addr]
		if !ok {
			// Account not yet resolved; resolve it first
			account, err := r.mainTrie.GetAccount(addr)
			if err != nil {
				return common.Hash{}, err
			}
			if account == nil {
				root = types.EmptyRootHash
			} else {
				root = account.Root
			}
			r.subRoots[addr] = root
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

// Code implements ContractCodeReader. It delegates to the inner reader and
// records the code as a keccak256 preimage so the validator can access it.
func (r *recordingReader) Code(addr common.Address, codeHash common.Hash) ([]byte, error) {
	code, err := r.innerReader.Code(addr, codeHash)
	if err != nil {
		return nil, err
	}
	if len(code) > 0 {
		hash := crypto.Keccak256Hash(code)
		r.db.recordingTrieDB.record(hash, code)
	}
	return code, nil
}

// CodeSize implements ContractCodeReader. It delegates to the inner reader.
func (r *recordingReader) CodeSize(addr common.Address, codeHash common.Hash) (int, error) {
	// We don't need to record code for size-only queries since the validator
	// will also call Code() for any code it needs.
	return r.innerReader.CodeSize(addr, codeHash)
}
