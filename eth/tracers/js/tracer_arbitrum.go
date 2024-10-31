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

package js

import (
	"math/big"

	"github.com/dop251/goja"
	"github.com/ethereum/go-ethereum/common"
)

func (jst *jsTracer) CaptureArbitrumTransfer(
	from, to *common.Address, value *big.Int, before bool, purpose string,
) {
	traceTransfer, ok := goja.AssertFunction(jst.obj.Get("captureArbitrumTransfer"))
	if !ok {
		return
	}

	transfer := jst.vm.NewObject()
	if from != nil {
		transfer.Set("from", from.String())
	} else {
		transfer.Set("from", nil)
	}
	if to != nil {
		transfer.Set("to", to.String())
	} else {
		transfer.Set("to", nil)
	}

	transfer.Set("value", value)
	transfer.Set("before", before)
	transfer.Set("purpose", purpose)

	if _, err := traceTransfer(transfer); err != nil {
		jst.err = wrapError("captureArbitrumTransfer", err)
	}
}

func (jst *jsTracer) CaptureStylusHostio(name string, args, outs []byte, startInk, endInk uint64) {
	hostio, ok := goja.AssertFunction(jst.obj.Get("hostio"))
	if !ok {
		return
	}

	info := jst.vm.NewObject()
	info.Set("name", name)
	info.Set("args", args)
	info.Set("outs", outs)
	info.Set("startInk", startInk)
	info.Set("endInk", endInk)

	if _, err := hostio(jst.obj, info); err != nil {
		jst.err = wrapError("hostio", err)
	}
}
