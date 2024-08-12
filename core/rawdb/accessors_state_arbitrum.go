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
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

type Target string

const (
	TargetWavm  Target = "wavm"
	TargetArm64 Target = "arm64"
	TargetAmd64 Target = "amd64"
	TargetHost  Target = "host"
)

func (t Target) ToString() string {
	return string(t)
}

func TargetFromString(str string) (Target, error) {
	target := Target(str)
	switch target {
	case TargetWavm, TargetArm64, TargetAmd64, TargetHost:
		return target, nil
	default:
		return target, errors.New("unsupported target")
	}
}

var Targets = []Target{TargetWavm, TargetArm64, TargetAmd64, TargetHost}

func WriteActivation(db ethdb.KeyValueWriter, moduleHash common.Hash, asmMap map[Target][]byte) {
	for target, asm := range asmMap {
		WriteActivatedAsm(db, target, moduleHash, asm)
	}
}

// Stores the activated asm for a given moduleHash and target
func WriteActivatedAsm(db ethdb.KeyValueWriter, target Target, moduleHash common.Hash, asm []byte) {
	var prefix []byte
	switch target {
	case TargetWavm:
		prefix = activatedAsmWavmPrefix
	case TargetArm64:
		prefix = activatedAsmArmPrefix
	case TargetAmd64:
		prefix = activatedAsmX86Prefix
	case TargetHost:
		prefix = activatedAsmHostPrefix
	default:
		log.Crit("Failed to store activated wasm asm, invalid target specified", "target", target)
	}
	key := activatedKey(prefix, moduleHash)
	if err := db.Put(key[:], asm); err != nil {
		log.Crit("Failed to store activated wasm asm", "err", err)
	}
}

func ReadActivatedAsm(db ethdb.KeyValueReader, target Target, moduleHash common.Hash) []byte {
	var prefix []byte
	switch target {
	case TargetWavm:
		prefix = activatedAsmWavmPrefix
	case TargetArm64:
		prefix = activatedAsmArmPrefix
	case TargetAmd64:
		prefix = activatedAsmX86Prefix
	case TargetHost:
		prefix = activatedAsmHostPrefix
	default:
		log.Crit("Failed to store activated wasm asm, invalid target specified", "target", target)
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

// Retrieves wasm schema version, if the corresponding key is not found returns version 0
func ReadWasmSchemaVersion(db ethdb.KeyValueReader) []byte {
	version, err := db.Get(wasmSchemaVersionKey)
	if err != nil || len(version) == 0 {
		return []byte{0}
	}
	return version
}
