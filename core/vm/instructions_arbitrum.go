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

package vm

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

// adaptation of opBlockHash that doesn't require an EVM stack
func BlockHashOp(evm *EVM, block *big.Int) common.Hash {
	if !block.IsUint64() {
		return common.Hash{}
	}
	num64 := block.Uint64()
	upper, err := evm.ProcessingHook.L1BlockNumber(evm.Context)
	if err != nil {
		return common.Hash{}
	}

	var lower uint64
	if upper < 257 {
		lower = 0
	} else {
		lower = upper - 256
	}
	if num64 >= lower && num64 < upper {
		hash, err := evm.ProcessingHook.L1BlockHash(evm.Context, num64)
		if err != nil {
			return common.Hash{}
		}
		return hash
	}
	return common.Hash{}
}
