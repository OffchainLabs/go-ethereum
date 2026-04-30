package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func (db *CachingDB) ActivatedAsm(target rawdb.WasmTarget, moduleHash common.Hash) []byte {
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

// arbNodeConfig is set once during ExecutionEngine.Initialize (before transaction
// processing starts) and only read afterward, so atomic access is not needed.
// Geth treats the value as opaque; Nitro asserts it back to its typed config struct
// at the read site.
func (db *CachingDB) ArbNodeConfig() any       { return db.arbNodeConfig }
func (db *CachingDB) SetArbNodeConfig(cfg any) { db.arbNodeConfig = cfg }
