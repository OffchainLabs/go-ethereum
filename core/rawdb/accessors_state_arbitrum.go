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

// Stores the activated asm and module for a given codeHash
func WriteActivation(db ethdb.KeyValueWriter, moduleHash common.Hash, asm, module []byte) {
	key := ActivatedAsmKey(moduleHash)
	if err := db.Put(key[:], asm); err != nil {
		log.Crit("Failed to store activated wasm asm", "err", err)
	}

	key = ActivatedModuleKey(moduleHash)
	if err := db.Put(key[:], module); err != nil {
		log.Crit("Failed to store activated wasm module", "err", err)
	}
}
