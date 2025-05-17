// Copyright 2022 The go-ethereum Authors
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

package native

import (
	"math/big"

	"github.com/curtis0505/arbitrum/common"
	"github.com/curtis0505/arbitrum/core/vm"
)

type arbitrumTransfer struct {
	Purpose string  `json:"purpose"`
	From    *string `json:"from"`
	To      *string `json:"to"`
	Value   string  `json:"value"`
}

func (t *callTracer) CaptureArbitrumTransfer(
	env *vm.EVM, from, to *common.Address, value *big.Int, before bool, purpose string,
) {
	transfer := arbitrumTransfer{
		Purpose: purpose,
		Value:   bigToHex(value),
	}
	if from != nil {
		from := from.String()
		transfer.From = &from
	}
	if to != nil {
		to := to.String()
		transfer.To = &to
	}
	if before {
		t.beforeEVMTransfers = append(t.beforeEVMTransfers, transfer)
	} else {
		t.afterEVMTransfers = append(t.afterEVMTransfers, transfer)
	}
}

func (*fourByteTracer) CaptureArbitrumTransfer(env *vm.EVM, from, to *common.Address, value *big.Int, before bool, purpose string) {
}
func (*noopTracer) CaptureArbitrumTransfer(env *vm.EVM, from, to *common.Address, value *big.Int, before bool, purpose string) {
}
func (*prestateTracer) CaptureArbitrumTransfer(env *vm.EVM, from, to *common.Address, value *big.Int, before bool, purpose string) {
}
func (*revertReasonTracer) CaptureArbitrumTransfer(env *vm.EVM, from, to *common.Address, value *big.Int, before bool, purpose string) {
}

func (*callTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)         {}
func (*fourByteTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)     {}
func (*noopTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)         {}
func (*prestateTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)     {}
func (*revertReasonTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool) {}

func (*callTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool)     {}
func (*fourByteTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {}
func (*noopTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool)     {}
func (*prestateTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {}
func (*revertReasonTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {
}
