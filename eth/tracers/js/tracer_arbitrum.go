// Copyright 2017 The go-ethereum Authors
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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
)

func (jst *jsTracer) CaptureArbitrumTransfer(
	env *vm.EVM, from, to *common.Address, value *big.Int, before bool, purpose vm.ArbTransferPurpose,
) {

	obj := jst.vm.PushObject()
	if from != nil {
		jst.addToObj(obj, "from", from.Hex())
	}
	if to != nil {
		jst.addToObj(obj, "to", to.Hex())
	}
	jst.addToObj(obj, "value", value)
	jst.addToObj(obj, "before", before)
	jst.addToObj(obj, "purpose", uint64(purpose))
	jst.vm.PutPropString(jst.stateObject, "transfer")

	if _, err := jst.call(true, "captureArbitrumTransfer", "transfer"); err != nil {
		jst.err = wrapError("captureArbitrumTransfer", err)
	}
}

func (*jsTracer) CaptureArbitrumStorageGet(key common.Hash, depth int, before bool)        {}
func (*jsTracer) CaptureArbitrumStorageSet(key, value common.Hash, depth int, before bool) {}
