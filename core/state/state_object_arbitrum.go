package state

import (
	"fmt"
	"github.com/ethereum/go-ethereum/common"
)

// CompiledWasmCode returns the user wasm contract code associated with this object, if any.
func (s *stateObject) CompiledWasmCode(db Database, previouslyExists bool) []byte {
	if s.compiledWasmCode != nil {
		return s.compiledWasmCode
	}
	if !previouslyExists {
		return nil
	}
	// We can't check the code hash for existance so instead we pass it in
	//if bytes.Equal(s.CodeHash(), emptyCodeHash) {
	//	return nil
	//}
	compiledWasmCode, err := db.CompiledWasmContractCode(s.addrHash, common.BytesToHash(s.CodeHash()))
	if err != nil {
		s.setError(fmt.Errorf("can't load code hash %x: %v", s.CodeHash(), err))
	}
	s.compiledWasmCode = compiledWasmCode
	return compiledWasmCode
}

func (s *stateObject) SetCompiledWasmCode(code []byte, previouslyExists bool) {
	prevcode := s.CompiledWasmCode(s.db.db, previouslyExists)
	s.db.journal.append(wasmCodeChange{
		account:  &s.address,
		prevcode: prevcode,
	})
	s.setWASMCode(code)
}

func (s *stateObject) setWASMCode(code []byte) {
	s.compiledWasmCode = code
	s.dirtyCompiledWasmCode = true
}
