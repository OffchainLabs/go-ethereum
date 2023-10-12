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
	activatedAsmPrefix    = []byte{0x00, 'w', 'a'} // (prefix, version, code_hash) -> stylus asm
	activatedModulePrefix = []byte{0x00, 'w', 'm'} // (prefix, version, code_hash) -> stylus module
)

// WasmKeyLen = CompiledWasmCodePrefix + version + hash
const WasmKeyLen = 3 + 2 + 32

type WasmKey = [WasmKeyLen]byte

func ActivatedAsmKey(version uint16, codeHash common.Hash) WasmKey {
	return newWasmKey(activatedAsmPrefix, version, codeHash)
}

func ActivatedModuleKey(version uint16, codeHash common.Hash) WasmKey {
	return newWasmKey(activatedModulePrefix, version, codeHash)
}

// key = prefix + version + hash
func newWasmKey(prefix []byte, version uint16, codeHash common.Hash) WasmKey {
	var key WasmKey
	copy(key[:3], prefix)
	binary.BigEndian.PutUint16(key[3:5], version)
	copy(key[5:], codeHash[:])
	return key
}

func IsActivatedAsmKey(key []byte) (bool, common.Hash, uint16) {
	return extractWasmKey(activatedAsmPrefix, key)
}

func IsActivatedModuleKey(key []byte) (bool, common.Hash, uint16) {
	return extractWasmKey(activatedModulePrefix, key)
}

func extractWasmKey(prefix, key []byte) (bool, common.Hash, uint16) {
	start := len(prefix)
	if bytes.HasPrefix(key, prefix) && len(key) == WasmKeyLen {
		version := binary.BigEndian.Uint16(key[start : start+2])
		codeHash := common.BytesToHash(key[start+2:])
		return true, codeHash, version
	}
	return false, common.Hash{}, 0
}
