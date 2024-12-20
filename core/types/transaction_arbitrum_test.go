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

package types

import "testing"

func TestTxCalldataUnitsCache(t *testing.T) {
	tx := &Transaction{}
	units := tx.GetCachedCalldataUnits(0)
	if units != nil {
		t.Errorf("unexpected initial cache present %v for compression 0", units)
	}
	units = tx.GetCachedCalldataUnits(1)
	if units != nil {
		t.Errorf("unexpected initial cache present %v for compression 1", units)
	}
	tx.SetCachedCalldataUnits(200, 1000)
	units = tx.GetCachedCalldataUnits(100)
	if units != nil {
		t.Errorf("unexpected cached units %v present for incorrect compression 100", units)
	}
	units = tx.GetCachedCalldataUnits(0)
	if units != nil {
		t.Errorf("unexpected cached units %v present for incorrect compression 0", units)
	}
	units = tx.GetCachedCalldataUnits(200)
	if units == nil || *units != 1000 {
		t.Errorf("unexpected cached units %v for correct compression 200", units)
	}
	tx.SetCachedCalldataUnits(1, 1<<60)
	units = tx.GetCachedCalldataUnits(1)
	if units != nil {
		t.Errorf("unexpected cache value %v present after reset", units)
	}
}
