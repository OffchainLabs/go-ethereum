package state

import (
	"errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

// CompiledWasmContractCode retrieves a particular contract's compiled wasm code.
func (db *cachingDB) CompiledWasmContractCode(addrHash, codeHash common.Hash) ([]byte, error) {
	if code := db.userWasmCache.Get(nil, codeHash.Bytes()); len(code) > 0 {
		return code, nil
	}
	code := rawdb.ReadCompiledWasmCode(db.db.DiskDB(), codeHash)
	if len(code) > 0 {
		db.userWasmCache.Set(codeHash.Bytes(), code)
		return code, nil
	}
	return nil, errors.New("not found")
}
