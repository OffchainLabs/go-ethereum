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
	TargetWavm  WasmTarget = "wavm"
	TargetArm64 WasmTarget = "arm64"
	TargetAmd64 WasmTarget = "amd64"
	TargetHost  WasmTarget = "host"
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
	default:
		return WasmPrefix{}, fmt.Errorf("invalid target: %v", target)
	}
	return prefix, nil
}

func IsSupportedWasmTarget(target WasmTarget) bool {
	_, err := activatedAsmKeyPrefix(target)
	return err == nil
}

func WriteActivation(db ethdb.KeyValueWriter, moduleHash common.Hash, asmMap map[WasmTarget][]byte) {
	for target, asm := range asmMap {
		WriteActivatedAsm(db, target, moduleHash, asm)
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
