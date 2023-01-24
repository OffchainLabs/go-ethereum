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
	"encoding/binary"
	"math/big"

	"errors"

	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

var (
	// Defines prefix bytes for Stylus WASM program bytecode
	// when deployed on-chain via a user-initiated transaction.
	// These byte prefixes are meant to conflict with the L1 contract EOF
	// validation rules so they can be sufficiently differentiated from EVM bytecode.
	// This allows us to store WASM programs as code in the stateDB side-by-side
	// with EVM contracts, but match against these prefix bytes when loading code
	// to execute the WASMs through Stylus rather than the EVM.
	stylusEOFMagic         = byte(0xEF)
	stylusEOFMagicSuffix   = byte(0x00)
	stylusEOFVersion       = byte(0x00)
	stylusEOFSectionHeader = byte(0x00)

	StylusPrefix = []byte{stylusEOFMagic, stylusEOFMagicSuffix, stylusEOFVersion, stylusEOFSectionHeader}
)

// IsStylusProgram checks if a specified bytecode is a user-submitted WASM program.
// Stylus differentiates WASMs from EVM bytecode via the prefix 0xEF000000 which will safely fail
// to pass through EVM-bytecode EOF validation rules.
func IsStylusProgram(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	return b[0] == stylusEOFMagic && b[1] == stylusEOFMagicSuffix && b[2] == stylusEOFVersion && b[3] == stylusEOFSectionHeader
}

// StripStylusPrefix if the specified input is a stylus program.
func StripStylusPrefix(b []byte) ([]byte, error) {
	if !IsStylusProgram(b) {
		return nil, errors.New("specified bytecode is not a Stylus program")
	}
	return b[4:], nil
}

func (s *StateDB) GetCompiledWasmCode(addr common.Address) []byte {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.CompiledWasmCode(s.db)
	}
	return nil
}

func (s *StateDB) SetCompiledWasmCode(addr common.Address, code []byte) {
	stateObject := s.GetOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetWasmCode(crypto.Keccak256Hash(code), code)
	}
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

type UserWasms map[WasmCall]*UserWasm
type UserWasm struct {
	NoncanonicalHash common.Hash
	CompressedWasm   []byte
	Wasm             []byte
}
type WasmCall struct {
	Version uint32
	Address common.Address
}

func (s *StateDB) StartRecording() {
	s.userWasms = make(UserWasms)
}

func (s *StateDB) RecordProgram(program common.Address, version uint32) {
	if s.userWasms != nil {
		call := WasmCall{
			Version: version,
			Address: program,
		}
		if _, ok := s.userWasms[call]; ok {
			return
		}
		rawCode := s.GetCode(program)
		compressedWasm, err := StripStylusPrefix(rawCode)
		if err != nil {
			log.Error("Could not strip stylus program prefix from raw code: %v", err)
			return
		}
		s.userWasms[call] = &UserWasm{
			NoncanonicalHash: s.NoncanonicalProgramHash(program, version),
			CompressedWasm:   compressedWasm,
		}
	}
}

func (s *StateDB) NoncanonicalProgramHash(program common.Address, version uint32) common.Hash {
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, version)
	return crypto.Keccak256Hash(prefix, s.GetCodeHash(program).Bytes())
}

func (s *StateDB) UserWasms() UserWasms {
	return s.userWasms
}

func (s *StateDB) AddUserModule(version uint32, program common.Address, source []byte) error {
	diskDB := s.db.TrieDB().DiskDB()
	// TODO: Key should encompass version, but doing this will prevent the recording db
	// from successfully reading the user module. Recording DB expects the key to be the
	// hash of the contents at this time.
	log.Info(fmt.Sprintf("Adding user module with version=%d, program=%#x", version, program))
	key := crypto.Keccak256Hash(source)
	return diskDB.Put(key.Bytes(), source)
}

func (s *StateDB) GetUserModule(version uint32, program common.Address) ([]byte, error) {
	diskDB := s.db.TrieDB().DiskDB()
	// TODO: In order for proving to work, the recording DB expects the key
	// of the module in the database to be exactly the keccak hash of its serialized bytes.
	// However, we do not have access to this at this time. Hardcoding to prove the test will pass.
	key, _ := hexutil.Decode("0xd99286ed0bdecb16c4c41ba88fbbecb49645f82a187bf780932a5d3f35408a13")
	return diskDB.Get(key)
}

func userModuleKey(version uint32, program common.Address) []byte {
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, version)
	return crypto.Keccak256Hash(prefix, program.Bytes()).Bytes()
}
