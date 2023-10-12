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

type cachedActivation struct {
	asm    []byte
	module []byte
	dirty  bool
}
type activatedWasms map[uint16]*cachedActivation

func (c activatedWasms) Copy() activatedWasms {
	cpy := make(activatedWasms, len(c))
	for key, value := range c {
		cpy[key] = value
	}
	return cpy
}

func (s *stateObject) NewActivation(db Database, version uint16, asm, module []byte) {
	// Can only be activated once, so this must have been empty
	s.db.journal.append(wasmCodeChange{
		account: &s.address,
		version: version,
	})
	s.cacheActivation(version, asm, module)

	codeHash := common.BytesToHash(s.CodeHash())
	if err := db.NewActivation(version, codeHash, asm, module); err != nil {
		s.db.setError(fmt.Errorf("failed to write activation for %x %v: %v", s.CodeHash(), version, err))
	}
}

func (s *stateObject) ActivatedAsm(db Database, version uint16) []byte {
	cached := s.getActivation(version)
	if cached.asm != nil {
		return cached.asm
	}

	asm, err := db.ActivatedAsm(version, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.db.setError(fmt.Errorf("missing asm for %x %v: %v", s.CodeHash(), version, err))
	}
	cached.asm = asm
	return asm
}

func (s *stateObject) ActivatedModule(db Database, version uint16) []byte {
	cached := s.getActivation(version)
	if cached.module != nil {
		return cached.module
	}

	module, err := db.ActivatedModule(version, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.db.setError(fmt.Errorf("missing module for %x %v: %v", s.CodeHash(), version, err))
	}
	cached.module = module
	return module
}

func (s *stateObject) getActivation(version uint16) *cachedActivation {
	if wasm, ok := s.activatedWasms[version]; ok {
		return wasm
	}
	cached := &cachedActivation{
		dirty: false,
	}
	s.activatedWasms[version] = cached
	return cached
}

func (s *stateObject) cacheActivation(version uint16, asm, module []byte) {
	s.activatedWasms[version] = &cachedActivation{
		asm:    asm,
		module: module,
		dirty:  true,
	}
}
