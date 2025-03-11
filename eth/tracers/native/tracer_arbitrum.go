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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
)

type arbitrumTransfer struct {
	Purpose string  `json:"purpose"`
	From    *string `json:"from"`
	To      *string `json:"to"`
	Value   string  `json:"value"`
}

func (t *callTracer) CaptureArbitrumTransfer(from, to *common.Address, value *big.Int, before bool, reason tracing.BalanceChangeReason) {
	transfer := arbitrumTransfer{
		Purpose: reason.Str(),
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

func (t *flatCallTracer) CaptureArbitrumTransfer(from, to *common.Address, value *big.Int, before bool, reason tracing.BalanceChangeReason) {
	if t.interrupt.Load() {
		return
	}
	transfer := arbitrumTransfer{
		Purpose: reason.Str(),
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

func (t *prestateTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool) {
	t.lookupAccount(types.ArbosStateAddress)
	t.lookupStorage(types.ArbosStateAddress, key)
}

func (t *prestateTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {
	t.lookupAccount(types.ArbosStateAddress)
	t.lookupStorage(types.ArbosStateAddress, key)
}

func bigToHex(n *big.Int) string {
	if n == nil {
		return ""
	}
	return "0x" + n.Text(16)
}
