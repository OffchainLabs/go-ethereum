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

	"github.com/ethereum/go-ethereum/common"
)

var (
	activatedAsmPrefix    = []byte{0x00, 'w', 'a'} // (prefix, moduleHash) -> stylus asm
	activatedModulePrefix = []byte{0x00, 'w', 'm'} // (prefix, moduleHash) -> stylus module
)

// WasmKeyLen = CompiledWasmCodePrefix + moduleHash
const WasmKeyLen = 3 + 32

type WasmKey = [WasmKeyLen]byte

func ActivatedAsmKey(moduleHash common.Hash) WasmKey {
	return newWasmKey(activatedAsmPrefix, moduleHash)
}

func ActivatedModuleKey(moduleHash common.Hash) WasmKey {
	return newWasmKey(activatedModulePrefix, moduleHash)
}

// key = prefix + moduleHash
func newWasmKey(prefix []byte, moduleHash common.Hash) WasmKey {
	var key WasmKey
	copy(key[:3], prefix)
	copy(key[3:], moduleHash[:])
	return key
}

func IsActivatedAsmKey(key []byte) (bool, common.Hash) {
	return extractWasmKey(activatedAsmPrefix, key)
}

func IsActivatedModuleKey(key []byte) (bool, common.Hash) {
	return extractWasmKey(activatedModulePrefix, key)
}

func extractWasmKey(prefix, key []byte) (bool, common.Hash) {
	if !bytes.HasPrefix(key, prefix) || len(key) != WasmKeyLen {
		return false, common.Hash{}
	}
	return true, common.BytesToHash(key[len(prefix):])
}
