package state

import (
	"github.com/ethereum/go-ethereum/common"
)

type wasmActivation struct {
	moduleHash common.Hash
}

func (ch wasmActivation) revert(s *StateDB) {
	delete(s.arbExtraData.activatedWasms, ch.moduleHash)
}

func (ch wasmActivation) dirtied() *common.Address {
	return nil
}

// Updates the Rust-side recent program cache
var CacheWasmRust func(asm []byte, moduleHash common.Hash, version uint16, debug bool) = func([]byte, common.Hash, uint16, bool) {}
var EvictWasmRust func(moduleHash common.Hash, version uint16, debug bool) = func(common.Hash, uint16, bool) {}

type CacheWasm struct {
	ModuleHash common.Hash
	Version    uint16
	Debug      bool
}

func (ch CacheWasm) revert(s *StateDB) {
	EvictWasmRust(ch.ModuleHash, ch.Version, ch.Debug)
}

func (ch CacheWasm) dirtied() *common.Address {
	return nil
}

type EvictWasm struct {
	ModuleHash common.Hash
	Version    uint16
	Debug      bool
}

func (ch EvictWasm) revert(s *StateDB) {
	asm := s.GetActivatedAsm(ch.ModuleHash) // only happens in native mode
	CacheWasmRust(asm, ch.ModuleHash, ch.Version, ch.Debug)
}

func (ch EvictWasm) dirtied() *common.Address {
	return nil
}
