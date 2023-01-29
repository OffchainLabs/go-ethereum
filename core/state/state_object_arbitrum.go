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
	compiledWasmCode, err := db.CompiledWasmContractCode(common.BytesToHash(s.CodeHash()), version)
	if err != nil {
		s.setError(fmt.Errorf("can't load code hash %x: %v", s.CodeHash(), err))
	}
	s.compiledWasmCode[version] = CompiledWasmCache{
		code:  compiledWasmCode,
		dirty: false,
	}
	return compiledWasmCode
}

func (s *stateObject) SetCompiledWasmCode(code []byte, version uint32) {
	// Can only be compiled once, so if it's being compiled, it was previous empty
	s.db.journal.append(wasmCodeChange{
		account: &s.address,
		version: version,
	})
	s.setWASMCode(code, version)
}

func (s *stateObject) setWASMCode(code []byte, version uint32) {
	s.compiledWasmCode[version] = CompiledWasmCache{
		code:  code,
		dirty: true,
	}
}
