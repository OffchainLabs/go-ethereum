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
	"encoding/binary"
	"fmt"
	"runtime"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

type WasmTarget string

const (
	TargetWasm  WasmTarget = "wasm"
	TargetWavm  WasmTarget = "wavm"
	TargetArm64 WasmTarget = "arm64"
	TargetAmd64 WasmTarget = "amd64"
	TargetHost  WasmTarget = "host"

	TargetArm64Cranelift WasmTarget = "arm64-cranelift"
	TargetAmd64Cranelift WasmTarget = "amd64-cranelift"
	TargetHostCranelift  WasmTarget = "host-cranelift"
)

func LocalTarget() WasmTarget {
	if runtime.GOOS == "linux" {
		switch runtime.GOARCH {
		case "arm64":
			return TargetArm64
		case "amd64":
			return TargetAmd64
		}
	}
	return TargetHost
}

func activatedAsmKeyPrefix(target WasmTarget) (WasmPrefix, error) {
	var prefix WasmPrefix
	switch target {
	case TargetWavm:
		prefix = activatedAsmWavmPrefix
	case TargetArm64:
		prefix = activatedAsmArmPrefix
	case TargetAmd64:
		prefix = activatedAsmX86Prefix
	case TargetHost:
		prefix = activatedAsmHostPrefix
	case TargetArm64Cranelift:
		prefix = activatedAsmArmCraneliftPrefix
	case TargetAmd64Cranelift:
		prefix = activatedAsmX86CraneliftPrefix
	case TargetHostCranelift:
		prefix = activatedAsmHostCraneliftPrefix
	default:
		return WasmPrefix{}, fmt.Errorf("invalid target: %v", target)
	}
	return prefix, nil
}

func CraneliftTarget(target WasmTarget) (WasmTarget, error) {
	switch target {
	case TargetArm64:
		return TargetArm64Cranelift, nil
	case TargetAmd64:
		return TargetAmd64Cranelift, nil
	case TargetHost:
		return TargetHostCranelift, nil
	default:
		return "", fmt.Errorf("no cranelift target for: %v", target)
	}
}

func IsCraneliftTarget(target WasmTarget) bool {
	switch target {
	case TargetArm64Cranelift, TargetAmd64Cranelift, TargetHostCranelift:
		return true
	default:
		return false
	}
}

// SplitAsmMap separates a unified asmMap into consensus (non-cranelift) and
// cranelift entries. This allows callers to pass only consensus targets to
// ActivateWasm (which has consistency checks) while persisting cranelift
// entries separately to the wasm store.
func SplitAsmMap(asmMap map[WasmTarget][]byte) (consensus map[WasmTarget][]byte, cranelift map[WasmTarget][]byte) {
	consensus = make(map[WasmTarget][]byte)
	cranelift = make(map[WasmTarget][]byte)
	for target, asm := range asmMap {
		if IsCraneliftTarget(target) {
			cranelift[target] = asm
		} else {
			consensus[target] = asm
		}
	}
	return consensus, cranelift
}

// BaseTarget returns the singlepass base target for a cranelift target.
func BaseTarget(target WasmTarget) (WasmTarget, error) {
	switch target {
	case TargetArm64Cranelift:
		return TargetArm64, nil
	case TargetAmd64Cranelift:
		return TargetAmd64, nil
	case TargetHostCranelift:
		return TargetHost, nil
	default:
		return "", fmt.Errorf("not a cranelift target: %v", target)
	}
}

// DeduplicateAsmMap returns a map keyed by base (non-cranelift) targets, where
// each entry holds the best available ASM: singlepass if it exists, otherwise
// cranelift. Non-native targets (wasm, wavm) are passed through as-is.
// This ensures every compiled program is represented exactly once under its
// base target key, regardless of which compiler produced the ASM.
func DeduplicateAsmMap(asmMap map[WasmTarget][]byte) map[WasmTarget][]byte {
	result := make(map[WasmTarget][]byte, len(asmMap))
	// First pass: add all non-cranelift entries.
	for target, asm := range asmMap {
		if !IsCraneliftTarget(target) {
			result[target] = asm
		}
	}
	// Second pass: add cranelift entries only if the base target is missing.
	for target, asm := range asmMap {
		if IsCraneliftTarget(target) {
			base, err := BaseTarget(target)
			if err != nil {
				continue
			}
			if _, exists := result[base]; !exists {
				result[base] = asm
			}
		}
	}
	return result
}

func IsSupportedWasmTarget(target WasmTarget) bool {
	_, err := activatedAsmKeyPrefix(target)
	return err == nil
}

func WriteActivation(db ethdb.KeyValueWriter, moduleHash common.Hash, asmMap map[WasmTarget][]byte) {
	for target, asm := range asmMap {
		if target != TargetWasm {
			WriteActivatedAsm(db, target, moduleHash, asm)
		}
	}
}

// Stores the activated asm for a given moduleHash and target
func WriteActivatedAsm(db ethdb.KeyValueWriter, target WasmTarget, moduleHash common.Hash, asm []byte) {
	prefix, err := activatedAsmKeyPrefix(target)
	if err != nil {
		log.Crit("Failed to store activated wasm asm", "err", err)
	}
	key := activatedKey(prefix, moduleHash)
	if err := db.Put(key[:], asm); err != nil {
		log.Crit("Failed to store activated wasm asm", "err", err)
	}
}

// Retrieves the activated asm for a given moduleHash and target
func ReadActivatedAsm(db ethdb.KeyValueReader, target WasmTarget, moduleHash common.Hash) []byte {
	if target == TargetWasm {
		return nil // wasm is not stored in the database
	}
	prefix, err := activatedAsmKeyPrefix(target)
	if err != nil {
		log.Crit("Failed to read activated wasm asm", "err", err)
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
	if err := db.Put(wasmSchemaVersionKey, []byte{WasmSchemaVersion}); err != nil {
		log.Crit("Failed to store wasm schema version", "err", err)
	}
}

// Retrieves wasm schema version
func ReadWasmSchemaVersion(db ethdb.KeyValueReader) ([]byte, error) {
	return db.Get(wasmSchemaVersionKey)
}

// Stores wasmer serialize version
func WriteWasmerSerializeVersion(db ethdb.KeyValueWriter, wasmerSerializeVersion uint32) error {
	buf := make([]byte, 32)
	binary.BigEndian.PutUint32(buf, wasmerSerializeVersion)
	return db.Put(wasmerSerializeVersionKey, buf)
}

// Retrieves wasmer serialize version
func ReadWasmerSerializeVersion(db ethdb.KeyValueReader) (uint32, error) {
	buf, err := db.Get(wasmerSerializeVersionKey)
	if err != nil {
		return 0, err
	}
	version := binary.BigEndian.Uint32(buf)
	return version, nil
}
