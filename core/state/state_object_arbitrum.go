package state

import (
	"bytes"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
)

// CompiledWasmCode returns the user wasm contract code associated with this object, if any.
func (s *stateObject) CompiledWasmCode(db Database) []byte {
	if s.compiledWasmCode != nil {
		return s.compiledWasmCode
	}
	if bytes.Equal(s.CodeHash(), emptyCodeHash) {
		return nil
	}
	compiledWasmCode, err := db.CompiledWasmContractCode(s.addrHash, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.setError(fmt.Errorf("can't load code hash %x: %v", s.CodeHash(), err))
	}
	s.compiledWasmCode = compiledWasmCode
	return compiledWasmCode
}

func (s *stateObject) SetWasmCode(codeHash common.Hash, code []byte) {
	prevcode := s.CompiledWasmCode(s.db.db)
	s.db.journal.append(wasmCodeChange{
		account:  &s.address,
		prevhash: s.CodeHash(),
		prevcode: prevcode,
	})
	s.setWASMCode(codeHash, code)
}

func (s *stateObject) setWASMCode(codeHash common.Hash, code []byte) {
	s.compiledWasmCode = code
	s.data.CodeHash = codeHash[:]
	s.dirtyCompiledWasmCode = true
}
