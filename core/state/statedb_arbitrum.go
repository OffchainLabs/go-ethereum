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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

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
		s.userWasms[call] = &UserWasm{
			NoncanonicalHash: s.NoncanonicalProgramHash(program, version),
			CompressedWasm:   s.GetCode(program),
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
	key := userModuleKey(version, program)
	return diskDB.Put(key, source)
}

func (s *StateDB) GetUserModule(version uint32, program common.Address) ([]byte, error) {
	diskDB := s.db.TrieDB().DiskDB()
	key := userModuleKey(version, program)
	return diskDB.Get(key)
}

func userModuleKey(version uint32, program common.Address) []byte {
	prefix := make([]byte, 4)
	binary.BigEndian.PutUint32(prefix, version)
	return append(prefix, program.Bytes()...)
}
