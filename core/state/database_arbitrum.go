package state

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

// CompiledWasmContractCode retrieves a particular contract's compiled wasm code.
func (db *cachingDB) CompiledWasmContractCode(version uint32, codeHash common.Hash) ([]byte, error) {
	wasmKey := rawdb.CompiledWasmCodeKey(version, codeHash)
	if code, _ := db.compiledWasmCache.Get(wasmKey); len(code) > 0 {
		return code, nil
	}
	code, err := db.disk.Get(wasmKey[:])
	if err != nil {
		return nil, err
	}
	if len(code) > 0 {
		db.compiledWasmCache.Add(wasmKey, code)
		return code, nil
	}
	return nil, errors.New("not found")
}

func (db *cachingDB) SetCompiledWasmContractCode(version uint32, codeHash common.Hash, code []byte) error {
	wasmKey := rawdb.CompiledWasmCodeKey(version, codeHash)
	if code, _ := db.compiledWasmCache.Get(wasmKey); len(code) > 0 {
		return nil
	}
	db.compiledWasmCache.Add(wasmKey, code)
	return nil
}
