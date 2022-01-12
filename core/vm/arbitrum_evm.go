// Copyright 2014 The go-ethereum Authors
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
	"errors"
	"github.com/ethereum/go-ethereum/common"
)

type TxProcessingHook interface {
	StartTxHook() bool
	GasChargingHook(gasRemaining *uint64) error
	EndTxHook(totalGasUsed uint64, success bool)
	NonrefundableGas() uint64
	L1BlockNumber(blockCtx BlockContext) (uint64, error)
	L1BlockHash(blockCtx BlockContext, l1BlocKNumber uint64) (common.Hash, error)
}

type DefaultTxProcessor struct{}

func (p DefaultTxProcessor) StartTxHook() bool {
	return false
}

func (p DefaultTxProcessor) GasChargingHook(gasRemaining *uint64) error {
	return nil
}

func (p DefaultTxProcessor) EndTxHook(totalGasUsed uint64, success bool) {
	return
}

func (p DefaultTxProcessor) NonrefundableGas() uint64 {
	return 0
}

func (p DefaultTxProcessor) L1BlockNumber(blockCtx BlockContext) (uint64, error) {
	return blockCtx.BlockNumber.Uint64(), nil
}

func (p DefaultTxProcessor) L1BlockHash(blockCtx BlockContext, l1BlocKNumber uint64) (common.Hash, error) {
	var upper, lower uint64
	upper = blockCtx.BlockNumber.Uint64()
	if upper < 257 {
		lower = 0
	} else {
		lower = upper - 256
	}
	if l1BlocKNumber >= lower && l1BlocKNumber < upper {
		return blockCtx.GetHash(l1BlocKNumber), nil
	} else {
		return common.Hash{}, errors.New("invalid block number in blockhash request")
	}
}
