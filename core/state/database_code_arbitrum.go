package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

const (
	// Arbitrum: Cache size granted for caching clean compiled wasm code.
	activatedWasmCacheSize = 64 * 1024 * 1024
)

// Geth treats the config value as opaque; Nitro asserts it back to its typed config struct
// at the read site.
func (db *CodeDB) ArbNodeConfig() any       { return db.arbNodeConfig }
func (db *CodeDB) SetArbNodeConfig(cfg any) { db.arbNodeConfig = cfg }

func (db *CodeDB) ActivatedAsm(target rawdb.WasmTarget, moduleHash common.Hash) []byte {
	cacheKey := activatedAsmCacheKey{moduleHash, target}
	if asm, _ := db.activatedAsmCache.Get(cacheKey); len(asm) > 0 {
		return asm
	}
	asm := rawdb.ReadActivatedAsm(db.wasmdb, target, moduleHash)
	if len(asm) > 0 {
		db.activatedAsmCache.Add(cacheKey, asm)
	}
	return asm
}

func (db *CodeDB) WasmStore() ethdb.KeyValueStore {
	return db.wasmdb
}
