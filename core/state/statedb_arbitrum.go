// Copyright 2014 The go-ethereum Authors
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

// Package state provides a caching layer atop the Ethereum state trie.
package state

import (
	"bytes"
	"fmt"
	"math/big"

	"errors"
	"runtime"

	"github.com/paxosglobal/go-ethereum-arbitrum/common"
	"github.com/paxosglobal/go-ethereum-arbitrum/common/lru"
	"github.com/paxosglobal/go-ethereum-arbitrum/core/types"
	"github.com/paxosglobal/go-ethereum-arbitrum/log"
	"github.com/paxosglobal/go-ethereum-arbitrum/rlp"
	"github.com/paxosglobal/go-ethereum-arbitrum/trie"
)

var (
	// Defines prefix bytes for Stylus WASM program bytecode
	// when deployed on-chain via a user-initiated transaction.
	// These byte prefixes are meant to conflict with the L1 contract EOF
	// validation rules so they can be sufficiently differentiated from EVM bytecode.
	// This allows us to store WASM programs as code in the stateDB side-by-side
	// with EVM contracts, but match against these prefix bytes when loading code
	// to execute the WASMs through Stylus rather than the EVM.
	stylusEOFMagic       = byte(0xEF)
	stylusEOFMagicSuffix = byte(0xF0)
	stylusEOFVersion     = byte(0x00)
	// 4th byte specifies the Stylus dictionary used during compression

	StylusDiscriminant = []byte{stylusEOFMagic, stylusEOFMagicSuffix, stylusEOFVersion}
)

type ActivatedWasm struct {
	Asm    []byte
	Module []byte
}

// checks if a valid Stylus prefix is present
func IsStylusProgram(b []byte) bool {
	if len(b) < len(StylusDiscriminant)+1 {
		return false
	}
	return bytes.Equal(b[:3], StylusDiscriminant)
}

// strips the Stylus header from a contract, returning the dictionary used
func StripStylusPrefix(b []byte) ([]byte, byte, error) {
	if !IsStylusProgram(b) {
		return nil, 0, errors.New("specified bytecode is not a Stylus program")
	}
	return b[4:], b[3], nil
}

// creates a new Stylus prefix from the given dictionary byte
func NewStylusPrefix(dictionary byte) []byte {
	prefix := bytes.Clone(StylusDiscriminant)
	return append(prefix, dictionary)
}

func (s *StateDB) ActivateWasm(moduleHash common.Hash, asm, module []byte) {
	_, exists := s.arbExtraData.activatedWasms[moduleHash]
	if exists {
		return
	}
	s.arbExtraData.activatedWasms[moduleHash] = &ActivatedWasm{
		Asm:    asm,
		Module: module,
	}
	s.journal.append(wasmActivation{
		moduleHash: moduleHash,
	})
}

func (s *StateDB) TryGetActivatedAsm(moduleHash common.Hash) ([]byte, error) {
	info, exists := s.arbExtraData.activatedWasms[moduleHash]
	if exists {
		return info.Asm, nil
	}
	return s.db.ActivatedAsm(moduleHash)
}

func (s *StateDB) GetActivatedModule(moduleHash common.Hash) []byte {
	info, exists := s.arbExtraData.activatedWasms[moduleHash]
	if exists {
		return info.Module
	}
	code, err := s.db.ActivatedModule(moduleHash)
	if err != nil {
		s.setError(fmt.Errorf("failed to load module for %x: %v", moduleHash, err))
	}
	return code
}

func (s *StateDB) GetStylusPages() (uint16, uint16) {
	return s.arbExtraData.openWasmPages, s.arbExtraData.everWasmPages
}

func (s *StateDB) GetStylusPagesOpen() uint16 {
	return s.arbExtraData.openWasmPages
}

func (s *StateDB) SetStylusPagesOpen(open uint16) {
	s.arbExtraData.openWasmPages = open
}

// Tracks that `new` additional pages have been opened, returning the previous counts
func (s *StateDB) AddStylusPages(new uint16) (uint16, uint16) {
	open, ever := s.GetStylusPages()
	s.arbExtraData.openWasmPages = common.SaturatingUAdd(open, new)
	s.arbExtraData.everWasmPages = common.MaxInt(ever, s.arbExtraData.openWasmPages)
	return open, ever
}

func (s *StateDB) AddStylusPagesEver(new uint16) {
	s.arbExtraData.everWasmPages = common.SaturatingUAdd(s.arbExtraData.everWasmPages, new)
}

func NewDeterministic(root common.Hash, db Database) (*StateDB, error) {
	sdb, err := New(root, db, nil)
	if err != nil {
		return nil, err
	}
	sdb.deterministic = true
	return sdb, nil
}

func (s *StateDB) Deterministic() bool {
	return s.deterministic
}

type ArbitrumExtraData struct {
	unexpectedBalanceDelta *big.Int                       // total balance change across all accounts
	userWasms              UserWasms                      // user wasms encountered during execution
	openWasmPages          uint16                         // number of pages currently open
	everWasmPages          uint16                         // largest number of pages ever allocated during this tx's execution
	activatedWasms         map[common.Hash]*ActivatedWasm // newly activated WASMs
	recentWasms            RecentWasms
}

func (s *StateDB) SetArbFinalizer(f func(*ArbitrumExtraData)) {
	runtime.SetFinalizer(s.arbExtraData, f)
}

func (s *StateDB) GetCurrentTxLogs() []*types.Log {
	return s.logs[s.thash]
}

// GetUnexpectedBalanceDelta returns the total unexpected change in balances since the last commit to the database.
func (s *StateDB) GetUnexpectedBalanceDelta() *big.Int {
	return new(big.Int).Set(s.arbExtraData.unexpectedBalanceDelta)
}

func (s *StateDB) GetSelfDestructs() []common.Address {
	selfDestructs := []common.Address{}
	for addr := range s.journal.dirties {
		obj, exist := s.stateObjects[addr]
		if !exist {
			continue
		}
		if obj.selfDestructed {
			selfDestructs = append(selfDestructs, addr)
		}
	}
	return selfDestructs
}

// making the function public to be used by external tests
func ForEachStorage(s *StateDB, addr common.Address, cb func(key, value common.Hash) bool) error {
	return forEachStorage(s, addr, cb)
}

// moved here from statedb_test.go
func forEachStorage(s *StateDB, addr common.Address, cb func(key, value common.Hash) bool) error {
	so := s.getStateObject(addr)
	if so == nil {
		return nil
	}
	tr, err := so.getTrie()
	if err != nil {
		return err
	}
	trieIt, err := tr.NodeIterator(nil)
	if err != nil {
		return err
	}
	it := trie.NewIterator(trieIt)

	for it.Next() {
		key := common.BytesToHash(s.trie.GetKey(it.Key))
		if value, dirty := so.dirtyStorage[key]; dirty {
			if !cb(key, value) {
				return nil
			}
			continue
		}

		if len(it.Value) > 0 {
			_, content, _, err := rlp.Split(it.Value)
			if err != nil {
				return err
			}
			if !cb(key, common.BytesToHash(content)) {
				return nil
			}
		}
	}
	return nil
}

// maps moduleHash to activation info
type UserWasms map[common.Hash]ActivatedWasm

func (s *StateDB) StartRecording() {
	s.arbExtraData.userWasms = make(UserWasms)
}

func (s *StateDB) RecordProgram(moduleHash common.Hash) {
	asm, err := s.TryGetActivatedAsm(moduleHash)
	if err != nil {
		log.Crit("can't find activated wasm while recording", "modulehash", moduleHash)
	}
	if s.arbExtraData.userWasms != nil {
		s.arbExtraData.userWasms[moduleHash] = ActivatedWasm{
			Asm:    asm,
			Module: s.GetActivatedModule(moduleHash),
		}
	}
}

func (s *StateDB) UserWasms() UserWasms {
	return s.arbExtraData.userWasms
}

func (s *StateDB) RecordCacheWasm(wasm CacheWasm) {
	s.journal.entries = append(s.journal.entries, wasm)
}

func (s *StateDB) RecordEvictWasm(wasm EvictWasm) {
	s.journal.entries = append(s.journal.entries, wasm)
}

func (s *StateDB) GetRecentWasms() RecentWasms {
	return s.arbExtraData.recentWasms
}

// Type for managing recent program access.
// The cache contained is discarded at the end of each block.
type RecentWasms struct {
	cache *lru.BasicLRU[common.Hash, struct{}]
}

// Creates an un uninitialized cache
func NewRecentWasms() RecentWasms {
	return RecentWasms{cache: nil}
}

// Inserts a new item, returning true if already present.
func (p RecentWasms) Insert(item common.Hash, retain uint16) bool {
	if p.cache == nil {
		cache := lru.NewBasicLRU[common.Hash, struct{}](int(retain))
		p.cache = &cache
	}
	if _, hit := p.cache.Get(item); hit {
		return hit
	}
	p.cache.Add(item, struct{}{})
	return false
}

// Copies all entries into a new LRU.
func (p RecentWasms) Copy() RecentWasms {
	if p.cache == nil {
		return NewRecentWasms()
	}
	cache := lru.NewBasicLRU[common.Hash, struct{}](p.cache.Capacity())
	for _, item := range p.cache.Keys() {
		cache.Add(item, struct{}{})
	}
	return RecentWasms{cache: &cache}
}
