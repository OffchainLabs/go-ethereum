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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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

	StylusPrefix = []byte{stylusEOFMagic, stylusEOFMagicSuffix, stylusEOFVersion}
)

type ActivatedWasm struct {
	Asm    []byte
	Module []byte
}

// IsStylusProgram checks if a specified bytecode is a user-submitted WASM program.
// Stylus differentiates WASMs from EVM bytecode via the prefix 0xEFF000 which will safely fail
// to pass through EVM-bytecode EOF validation rules.
func IsStylusProgram(b []byte) bool {
	if len(b) < len(StylusPrefix) {
		return false
	}
	return bytes.Equal(b[:3], StylusPrefix)
}

// StripStylusPrefix if the specified input is a stylus program.
func StripStylusPrefix(b []byte) ([]byte, error) {
	if !IsStylusProgram(b) {
		return nil, errors.New("specified bytecode is not a Stylus program")
	}
	return b[3:], nil
}

func (s *StateDB) ActivateWasm(moduleHash common.Hash, asm, module []byte) {
	_, exists := s.activatedWasms[moduleHash]
	if exists {
		return
	}
	s.activatedWasms[moduleHash] = &ActivatedWasm{
		Asm:    asm,
		Module: module,
	}
	s.journal.append(wasmActivation{
		moduleHash: moduleHash,
	})
}

func (s *StateDB) GetActivatedAsm(moduleHash common.Hash) []byte {
	info, exists := s.activatedWasms[moduleHash]
	if exists {
		return info.Asm
	}
	asm, err := s.db.ActivatedAsm(moduleHash)
	if err != nil {
		s.setError(fmt.Errorf("failed to load asm for %x: %v", moduleHash, err))
	}
	return asm
}

func (s *StateDB) GetActivatedModule(moduleHash common.Hash) []byte {
	info, exists := s.activatedWasms[moduleHash]
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
	return s.openWasmPages, s.everWasmPages
}

func (s *StateDB) GetStylusPagesOpen() uint16 {
	return s.openWasmPages
}

func (s *StateDB) SetStylusPagesOpen(open uint16) {
	s.openWasmPages = open
}

// Tracks that `new` additional pages have been opened, returning the previous counts
func (s *StateDB) AddStylusPages(new uint16) (uint16, uint16) {
	open, ever := s.GetStylusPages()
	s.openWasmPages = common.SaturatingUAdd(open, new)
	s.everWasmPages = common.MaxInt(ever, s.openWasmPages)
	return open, ever
}

func (s *StateDB) AddStylusPagesEver(new uint16) {
	s.everWasmPages = common.SaturatingUAdd(s.everWasmPages, new)
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

func (s *StateDB) GetCurrentTxLogs() []*types.Log {
	return s.logs[s.thash]
}

// GetUnexpectedBalanceDelta returns the total unexpected change in balances since the last commit to the database.
func (s *StateDB) GetUnexpectedBalanceDelta() *big.Int {
	return new(big.Int).Set(s.unexpectedBalanceDelta)
}

func (s *StateDB) GetSuicides() []common.Address {
	suicides := []common.Address{}
	for addr := range s.journal.dirties {
		obj, exist := s.stateObjects[addr]
		if !exist {
			continue
		}
		if obj.suicided {
			suicides = append(suicides, addr)
		}
	}
	return suicides
}

// maps moduleHash to activation info
type UserWasms map[common.Hash]ActivatedWasm

func (s *StateDB) StartRecording() {
	s.userWasms = make(UserWasms)
}

func (s *StateDB) RecordProgram(moduleHash common.Hash) {
	if s.userWasms != nil {
		s.userWasms[moduleHash] = ActivatedWasm{
			Asm:    s.GetActivatedAsm(moduleHash),
			Module: s.GetActivatedModule(moduleHash),
		}
	}
}

func (s *StateDB) UserWasms() UserWasms {
	return s.userWasms
}
