// Copyright 2015 The go-ethereum Authors
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
)

func TestBurnGasOutOfGas(t *testing.T) {
	const (
		forwarded     = 1000
		initReservoir = 500
		priorRegular  = 300
		priorState    = 100
	)
	parent := GasBudget{RegularGas: forwarded, StateGas: initReservoir}
	child := parent.Forward(forwarded)

	c := &Contract{Gas: child}
	if _, ok := c.Gas.Charge(GasCosts{RegularGas: priorRegular, StateGas: priorState}); !ok {
		t.Fatal("setup charge failed")
	}
	remaining := c.Gas.RegularGas

	if err := c.BurnGas(remaining + 1); err != ErrOutOfGas {
		t.Fatalf("BurnGas error = %v, want ErrOutOfGas", err)
	}
	if c.Gas.RegularGas != 0 {
		t.Errorf("RegularGas = %d, want 0", c.Gas.RegularGas)
	}
	if want := priorRegular + remaining; c.Gas.UsedRegularGas != uint64(want) {
		t.Errorf("UsedRegularGas = %d, want %d", c.Gas.UsedRegularGas, want)
	}
	if c.Gas.StateGas != initReservoir-priorState {
		t.Errorf("StateGas = %d, want %d", c.Gas.StateGas, initReservoir-priorState)
	}
	if c.Gas.UsedStateGas != priorState {
		t.Errorf("UsedStateGas = %d, want %d", c.Gas.UsedStateGas, priorState)
	}

	parent.Absorb(c.Gas.ExitHalt(initReservoir), forwarded)
	if parent.UsedRegularGas != forwarded {
		t.Errorf("parent UsedRegularGas = %d after absorb, want %d (no phantom spillover)", parent.UsedRegularGas, forwarded)
	}
	if parent.StateGas != initReservoir {
		t.Errorf("parent StateGas = %d after absorb, want %d", parent.StateGas, initReservoir)
	}
}
