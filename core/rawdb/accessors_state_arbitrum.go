// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

const (
	AsmArm  = "arm"
	AsmX86  = "x86"
	AsmHost = "host"
)

// Stores the activated module for a given moduleHash
func WriteActivatedModule(db ethdb.KeyValueWriter, moduleHash common.Hash, module []byte) {
	key := activatedKey(activatedModulePrefix, moduleHash)
	if err := db.Put(key[:], module); err != nil {
		log.Crit("Failed to store activated wasm module", "err", err)
	}
}

func ReadActivatedModule(db ethdb.KeyValueReader, moduleHash common.Hash) []byte {
	key := activatedKey(activatedModulePrefix, moduleHash)
	module, err := db.Get(key[:])
	if err != nil {
		return nil
	}
	return module
}

// Stores the activated asm for a given arch and moduleHash
func WriteActivatedAsm(db ethdb.KeyValueWriter, arch string, moduleHash common.Hash, asm []byte) {
	var prefix []byte
	switch arch {
	case AsmArm:
		prefix = activatedAsmArmPrefix
	case AsmX86:
		prefix = activatedAsmX86Prefix
	case AsmHost:
		prefix = activatedAsmHostPrefix
	default:
		log.Crit("Failed to store activated wasm asm, invalid arch specified", "arch", arch)
	}
	key := activatedKey(prefix, moduleHash)
	if err := db.Put(key[:], asm); err != nil {
		log.Crit("Failed to store activated wasm asm", "err", err)
	}
}

func ReadActivatedAsm(db ethdb.KeyValueReader, arch string, moduleHash common.Hash) []byte {
	var prefix []byte
	switch arch {
	case AsmArm:
		prefix = activatedAsmArmPrefix
	case AsmX86:
		prefix = activatedAsmX86Prefix
	case AsmHost:
		prefix = activatedAsmHostPrefix
	default:
		log.Crit("Failed to store activated wasm asm, invalid arch specified", "arch", arch)
	}
	key := activatedKey(prefix, moduleHash)
	asm, err := db.Get(key[:])
	if err != nil {
		return nil
	}
	return asm
}

// Stores wasm schema version
func WriteWasmSchemaVersion(db ethdb.KeyValueWriter) {
	if err := db.Put(wasmSchemaVersionKey, []byte{wasmSchemaVersion}); err != nil {
		log.Crit("Failed to store wasm schema version", "err", err)
	}
}

// Retrieves wasm schema version, if the correspoding key is not foud returns version 0
func ReadWasmSchemaVersion(db ethdb.KeyValueReader) byte {
	version, err := db.Get(wasmSchemaVersionKey)
	if err != nil || len(version) == 0 {
		return 0
	} else if len(version) != 1 {
		log.Crit("Invalid wasm schema version in database", "version", version)
	}
	return version[0]
}
