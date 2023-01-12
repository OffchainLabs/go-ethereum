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
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
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

func (s *StateDB) StartRecording() {
	s.programs = []common.Address{}
}

func (s *StateDB) RecordProgram(program common.Address) {
	if s.programs != nil {
		s.programs = append(s.programs, program)
		println("RECORDED PROGRAM ", program.Hex())
	}
}

func (s *StateDB) RecordedPrograms() [][]byte {
	programs := [][]byte{}
	if s.programs != nil {
		for _, program := range s.programs {
			programs = append(programs, s.GetCode(program))
		}
	}
	return programs
}

// TODO: move to ArbDB
var modules = make(map[common.Address][]byte)

func (s *StateDB) AddUserModule(version uint32, program common.Address, source []byte) {
	modules[program] = source
}

func (s *StateDB) GetUserModule(version uint32, program common.Address) ([]byte, error) {
	machine, ok := modules[program]
	if !ok {
		return nil, errors.New("no program for given address")
	}
	return machine, nil
}
