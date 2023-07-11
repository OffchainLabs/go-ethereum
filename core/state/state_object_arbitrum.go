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

package state

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

type CompiledWasmCache struct {
	code  Code
	dirty bool
}
type CompiledWasms map[uint32]CompiledWasmCache

func (c CompiledWasms) Copy() CompiledWasms {
	cpy := make(CompiledWasms, len(c))
	for key, value := range c {
		cpy[key] = value
	}

	return cpy
}

// CompiledWasmCode returns the user wasm contract code associated with this object, if any.
func (s *stateObject) CompiledWasmCode(db Database, version uint32) []byte {
	if wasm, ok := s.compiledWasmCode[version]; ok {
		return wasm.code
	}
	if version == 0 {
		return nil
	}
	compiledWasmCode, err := db.CompiledWasmContractCode(version, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.db.setError(fmt.Errorf("can't load code hash %x: %v", s.CodeHash(), err))
	}
	s.compiledWasmCode[version] = CompiledWasmCache{
		code:  compiledWasmCode,
		dirty: false,
	}
	return compiledWasmCode
}

func (s *stateObject) SetCompiledWasmCode(db Database, code []byte, version uint32) {
	// Can only be compiled once, so if it's being compiled, it was previous empty
	s.db.journal.append(wasmCodeChange{
		account: &s.address,
		version: version,
	})
	s.setWASMCode(code, version)
	if err := db.SetCompiledWasmContractCode(version, common.BytesToHash(s.CodeHash()), code); err != nil {
		s.setError(fmt.Errorf("cannot set compiled wasm contract code %x: %v", s.CodeHash(), err))
	}
}

func (s *stateObject) setWASMCode(code []byte, version uint32) {
	s.compiledWasmCode[version] = CompiledWasmCache{
		code:  code,
		dirty: true,
	}
}
