package state

import (
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func (db *cachingDB) NewActivation(moduleHash common.Hash, asm, module []byte) error {
	asmKey := rawdb.ActivatedAsmKey(moduleHash)
	modKey := rawdb.ActivatedModuleKey(moduleHash)
	if asm, _ := db.activatedAsmCache.Get(asmKey); len(asm) > 0 {
		return nil // already added
	}
	db.activatedAsmCache.Add(asmKey, asm)
	db.activatedModuleCache.Add(modKey, module)
	return nil
}

func (db *cachingDB) ActivatedAsm(moduleHash common.Hash) ([]byte, error) {
	wasmKey := rawdb.ActivatedAsmKey(moduleHash)
	if asm, _ := db.activatedAsmCache.Get(wasmKey); len(asm) > 0 {
		return asm, nil
	}
	asm, err := db.disk.Get(wasmKey[:])
	if err != nil {
		return nil, err
	}
	if len(asm) > 0 {
		db.activatedAsmCache.Add(wasmKey, asm)
		return asm, nil
	}
	return nil, errors.New("not found")
}

func (db *cachingDB) ActivatedModule(moduleHash common.Hash) ([]byte, error) {
	wasmKey := rawdb.ActivatedModuleKey(moduleHash)
	if module, _ := db.activatedModuleCache.Get(wasmKey); len(module) > 0 {
		return module, nil
	}
	module, err := db.disk.Get(wasmKey[:])
	if err != nil {
		return nil, err
	}
	if len(module) > 0 {
		db.activatedModuleCache.Add(wasmKey, module)
		return module, nil
	}
	return nil, errors.New("not found")
}
