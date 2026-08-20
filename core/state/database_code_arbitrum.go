package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// Arbitrum: Cache size granted for caching clean compiled wasm code.
	activatedWasmCacheSize = 64 * 1024 * 1024
)

type arbNodeConfigHolder struct {
	config any
}

// Geth treats the config value as opaque; Nitro asserts it back to its typed config struct
// at the read site.
func (db *CodeDB) ArbNodeConfig() any {
	holder := db.arbNodeConfig.Load()
	if holder == nil {
		return nil
	}
	return holder.config
}

// SetArbNodeConfig publishes the node config exactly once. The config is immutable
// after publication so all users of this shared CodeDB observe the same value.
func (db *CodeDB) SetArbNodeConfig(cfg any) {
	holder := &arbNodeConfigHolder{config: cfg}
	if !db.arbNodeConfig.CompareAndSwap(nil, holder) {
		panic("ArbNodeConfig already set")
	}
}

func (db *CodeDB) ActivatedAsm(target rawdb.WasmTarget, moduleHash common.Hash) []byte {
	cacheKey := activatedAsmCacheKey{moduleHash, target}
	if asm, _ := db.activatedAsmCache.Get(cacheKey); len(asm) > 0 {
		return asm
	}
	asm, err := rawdb.ReadActivatedAsm(db.wasmdb, target, moduleHash)
	if err == nil && len(asm) > 0 {
		db.activatedAsmCache.Add(cacheKey, asm)
	}
	if err != nil {
		log.Warn("failed reading activated asm", "err", err)
	}
	return asm
}

func (db *CodeDB) WasmStore() ethdb.KeyValueStore {
	return db.wasmdb
}
