// Copyright 2018 The go-ethereum Authors
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

// Package rawdb contains a collection of low level database accessors.

package rawdb

import (
	"github.com/ethereum/go-ethereum/common"
)

const WasmSchemaVersion byte = 0x01

const WasmPrefixLen = 3

// WasmKeyLen = CompiledWasmCodePrefix + moduleHash
const WasmKeyLen = WasmPrefixLen + common.HashLength

type WasmPrefix = [WasmPrefixLen]byte
type WasmKey = [WasmKeyLen]byte

var (
	wasmSchemaVersionKey = []byte("WasmSchemaVersion")

	// 0x00 prefix to avoid conflicts when wasmdb is not separate database
	activatedAsmWavmPrefix = WasmPrefix{0x00, 'w', 'w'} // (prefix, moduleHash) -> stylus module (wavm)
	activatedAsmArmPrefix  = WasmPrefix{0x00, 'w', 'r'} // (prefix, moduleHash) -> stylus asm for ARM system
	activatedAsmX86Prefix  = WasmPrefix{0x00, 'w', 'x'} // (prefix, moduleHash) -> stylus asm for x86 system
	activatedAsmHostPrefix = WasmPrefix{0x00, 'w', 'h'} // (prefix, moduleHash) -> stylus asm for system other then ARM and x86
)

func DeprecatedPrefixesV0() (keyPrefixes [][]byte, keyLength int) {
	return [][]byte{
		// deprecated prefixes, used in version 0x00, purged in version 0x01
		[]byte{0x00, 'w', 'a'}, // ActivatedAsmPrefix
		[]byte{0x00, 'w', 'm'}, // ActivatedModulePrefix
	}, 3 + 32
}

// key = prefix + moduleHash
func activatedKey(prefix WasmPrefix, moduleHash common.Hash) WasmKey {
	var key WasmKey
	copy(key[:WasmPrefixLen], prefix[:])
	copy(key[WasmPrefixLen:], moduleHash[:])
	return key
}
