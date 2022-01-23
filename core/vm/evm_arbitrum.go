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
	"github.com/ethereum/go-ethereum/common"
)

// Depth returns the current depth
func (evm *EVM) Depth() int {
	return evm.depth
}

type TxProcessingHook interface {
	StartTxHook() (bool, uint64, error, []byte) // return 4-tuple rather than *struct to avoid an import cycle
	GasChargingHook(gasRemaining *uint64) error
	EndTxHook(totalGasUsed uint64, transitionSuccess bool, evmSuccess bool)
	NonrefundableGas() uint64
	PushCaller(addr common.Address)
	PopCaller()
	L1BlockNumber(blockCtx BlockContext) (uint64, error)
	L1BlockHash(blockCtx BlockContext, l1BlocKNumber uint64) (common.Hash, error)
}

type DefaultTxProcessor struct{}

func (p DefaultTxProcessor) StartTxHook() (bool, uint64, error, []byte) {
	return false, 0, nil, nil
}

func (p DefaultTxProcessor) GasChargingHook(gasRemaining *uint64) error {
	return nil
}

func (p DefaultTxProcessor) EndTxHook(totalGasUsed uint64, transitionSuccess bool, evmSuccess bool) {
}

func (p DefaultTxProcessor) NonrefundableGas() uint64 {
	return 0
}

func (p DefaultTxProcessor) PushCaller(addr common.Address) {
}

func (p DefaultTxProcessor) PopCaller() {
}

func (p DefaultTxProcessor) L1BlockNumber(blockCtx BlockContext) (uint64, error) {
	return blockCtx.BlockNumber.Uint64(), nil
}

func (p DefaultTxProcessor) L1BlockHash(blockCtx BlockContext, l1BlocKNumber uint64) (common.Hash, error) {
	return blockCtx.GetHash(l1BlocKNumber), nil
}
