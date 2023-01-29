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
	CompiledWasmCodePrefix = []byte("w") // CompiledWasmCodePrefix + code hash -> account compiled wasm code
)

// compiledWasmCodeKey = CompiledWasmCodePrefix + hash
func compiledWasmCodeKey(hash common.Hash, version uint32) []byte {
	var versionBytes [4]byte
	binary.BigEndian.PutUint32(versionBytes[:], version)
	return append(append(CompiledWasmCodePrefix, hash.Bytes()...), versionBytes[:]...)
}

// IsCompiledWasmCodeKey reports whether the given byte slice is the key of compiled wasm contract code,
// if so return the raw code hash and version as well.
func IsCompiledWasmCodeKey(key []byte) (bool, []byte, uint32) {
	if bytes.HasPrefix(key, CompiledWasmCodePrefix) && len(key) == common.HashLength+4+len(CompiledWasmCodePrefix) {
		endOfHashOffset := len(CodePrefix) + common.HashLength
		codeHash := key[len(CodePrefix):endOfHashOffset]
		version := binary.BigEndian.Uint32(key[endOfHashOffset:])
		return true, codeHash, version
	}
	return false, nil, 0
}
