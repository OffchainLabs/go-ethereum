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

type wasmActivation struct {
	asm        []byte
	module     []byte
	moduleHash common.Hash
	dirty      bool
}

func (s *stateObject) setActivation(moduleHash common.Hash, asm, module []byte) {
	s.activation = wasmActivation{
		asm:    asm,
		module: module,
		dirty:  true,
	}
}

func (s *stateObject) NewActivation(db Database, moduleHash common.Hash, asm, module []byte) {
	// Can only be activated once, so this must have been empty
	s.db.journal.append(wasmCodeChange{
		account:    &s.address,
		moduleHash: moduleHash,
	})
	s.setActivation(moduleHash, asm, module)

	if err := db.NewActivation(moduleHash, asm, module); err != nil {
		s.db.setError(fmt.Errorf("failed to write activation for %x: %v", moduleHash, err))
	}
}

func (s *stateObject) ActivatedAsm(db Database, moduleHash common.Hash) []byte {
	if s.activation.asm != nil {
		return s.activation.asm
	}
	asm, err := db.ActivatedAsm(moduleHash)
	if err != nil {
		s.db.setError(fmt.Errorf("missing asm for %x: %v", s.CodeHash(), err))
	}
	s.activation.asm = asm
	return asm
}

func (s *stateObject) ActivatedModule(db Database, moduleHash common.Hash) []byte {
	if s.activation.module != nil {
		return s.activation.module
	}
	module, err := db.ActivatedModule(moduleHash)
	if err != nil {
		s.db.setError(fmt.Errorf("missing module for %x: %v", s.CodeHash(), err))
	}
	s.activation.module = module
	return module
}
