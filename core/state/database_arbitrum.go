package state

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func (db *cachingDB) ActivatedAsm(moduleHash common.Hash) ([]byte, error) {
	if asm, _ := db.activatedAsmCache.Get(moduleHash); len(asm) > 0 {
		return asm, nil
	}
	wasmKey := rawdb.ActivatedAsmKey(moduleHash)
	asm, err := db.wasmdb.Get(wasmKey[:])
	if err != nil {
		return nil, err
	}
	if len(asm) > 0 {
		db.activatedAsmCache.Add(moduleHash, asm)
		return asm, nil
	}
	return nil, errors.New("not found")
}

func (db *cachingDB) ActivatedModule(moduleHash common.Hash) ([]byte, error) {
	if module, _ := db.activatedModuleCache.Get(moduleHash); len(module) > 0 {
		return module, nil
	}
	wasmKey := rawdb.ActivatedModuleKey(moduleHash)
	module, err := db.wasmdb.Get(wasmKey[:])
	if err != nil {
		return nil, err
	}
	if len(module) > 0 {
		db.activatedModuleCache.Add(moduleHash, module)
		return module, nil
	}
	return nil, errors.New("not found")
}
