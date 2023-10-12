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

func (s *StateDB) NewActivation(program common.Address, moduleHash common.Hash, asm, module []byte) {
	stateObject := s.GetOrNewStateObject(program)
	if stateObject != nil {
		stateObject.NewActivation(s.db, moduleHash, asm, module)
	}
}

func (s *StateDB) GetActivatedAsm(program common.Address, moduleHash common.Hash) []byte {
	stateObject := s.getStateObject(program)
	if stateObject != nil {
		return stateObject.ActivatedAsm(s.db, moduleHash)
	}
	return nil
}

func (s *StateDB) GetActivatedModule(addr common.Address, moduleHash common.Hash) []byte {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.ActivatedModule(s.db, moduleHash)
	}
	return nil
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

// maps moduleHash to any correct address (TODO: replace with hash set)
type UserWasms map[common.Hash]common.Address

func (s *StateDB) StartRecording() {
	s.userWasms = make(UserWasms)
}

func (s *StateDB) RecordProgram(program common.Address, moduleHash common.Hash) {
	if s.userWasms != nil {
		s.userWasms[moduleHash] = program
	}
}

func (s *StateDB) UserWasms() UserWasms {
	return s.userWasms
}
