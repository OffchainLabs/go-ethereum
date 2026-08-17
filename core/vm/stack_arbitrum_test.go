// Copyright 2026 The go-ethereum Authors
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
	"testing"

	"github.com/holiman/uint256"
)

func TestSetDataZeroValueStack(t *testing.T) {
	stack := &Stack{}
	args := []uint256.Int{*uint256.NewInt(1), *uint256.NewInt(2), *uint256.NewInt(3)}
	stack.SetData(args)
	data := stack.Data()
	if len(data) != 3 || data[0].Uint64() != 1 || data[1].Uint64() != 2 || data[2].Uint64() != 3 {
		t.Fatalf("unexpected stack data: %v", data)
	}
}

func TestSetDataGrowsArena(t *testing.T) {
	stack := &Stack{}
	stack.SetData([]uint256.Int{*uint256.NewInt(1)})

	bigger := make([]uint256.Int, 10)
	bigger[9] = *uint256.NewInt(42)
	stack.SetData(bigger)
	data := stack.Data()
	if len(data) != 10 || data[9].Uint64() != 42 {
		t.Fatalf("unexpected stack data after regrow: %v", data)
	}
}

func TestSetDataPooledStack(t *testing.T) {
	stack := newStackForTesting()
	stack.push(uint256.NewInt(7))
	stack.SetData([]uint256.Int{*uint256.NewInt(5)})
	data := stack.Data()
	if len(data) != 1 || data[0].Uint64() != 5 {
		t.Fatalf("unexpected pooled stack data: %v", data)
	}
	if stack.inner.top != stack.bottom+1 {
		t.Fatalf("arena top not updated: top=%d bottom=%d", stack.inner.top, stack.bottom)
	}
}
