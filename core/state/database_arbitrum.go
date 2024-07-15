package state

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func (db *cachingDB) ActivatedAsm(targetName string, moduleHash common.Hash) ([]byte, error) {
	if asm, _ := db.activatedAsmCache.Get(moduleHash); len(asm) > 0 {
		return asm, nil
	}
	if asm := rawdb.ReadActivatedAsm(db.wasmdb, targetName, moduleHash); len(asm) > 0 {
		db.activatedAsmCache.Add(moduleHash, asm)
		return asm, nil
	}
	return nil, errors.New("not found")
}

func (db *cachingDB) ActivatedModule(moduleHash common.Hash) ([]byte, error) {
	if module, _ := db.activatedModuleCache.Get(moduleHash); len(module) > 0 {
		return module, nil
	}
	if module := rawdb.ReadActivatedModule(db.wasmdb, moduleHash); len(module) > 0 {
		db.activatedModuleCache.Add(moduleHash, module)
		return module, nil
	}
	return nil, errors.New("not found")
}
