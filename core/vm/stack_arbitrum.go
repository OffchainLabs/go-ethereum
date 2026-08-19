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

import "github.com/holiman/uint256"

func (stack *Stack) SetData(data []uint256.Int) {
	need := stack.bottom + len(data)
	if stack.inner == nil {
		stack.inner = &stackArena{data: make([]uint256.Int, need)}
	} else if len(stack.inner.data) < need {
		stack.inner.data = append(stack.inner.data, make([]uint256.Int, need-len(stack.inner.data))...)
	}
	copy(stack.inner.data[stack.bottom:], data)
	stack.size = len(data)
	stack.inner.top = need
}
