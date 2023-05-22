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
	"bytes"
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
)

var (
	CompiledWasmCodePrefix = []byte{0x00, 'w'} // (prefix, version, code_hash) -> account compiled wasm code
)

// CompiledWasmCodeKey = CompiledWasmCodePrefix + version + hash
type WasmKey = [2 + 4 + 32]byte

// CompiledWasmCodeKey = CompiledWasmCodePrefix + version + hash
func CompiledWasmCodeKey(version uint32, hash common.Hash) WasmKey {
	var key WasmKey
	copy(key[:2], CompiledWasmCodePrefix)
	binary.BigEndian.PutUint32(key[2:6], version)
	copy(key[6:], hash[:])
	return key
}

// IsCompiledWasmCodeKey reports whether the given byte slice is the key of compiled wasm contract code,
// if so return the raw code hash and version as well.
func IsCompiledWasmCodeKey(key []byte) (bool, common.Hash, uint32) {

	wasmKeyLen := len(CompiledWasmCodePrefix) + 4 + common.HashLength
	start := len(CompiledWasmCodePrefix)

	if bytes.HasPrefix(key, CompiledWasmCodePrefix) && len(key) == wasmKeyLen {
		version := binary.BigEndian.Uint32(key[start : start+4])
		codeHash := common.BytesToHash(key[start+4:])
		return true, codeHash, version
	}
	return false, common.Hash{}, 0
}
