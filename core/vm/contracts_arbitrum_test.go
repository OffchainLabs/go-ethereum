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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// withSavedResolver lets a test overwrite the package-level
// arbOSPrecompileResolver without bleeding into other tests.
//
// Callers must not use t.Parallel(): the resolver is a package
// global, and a parallel test observing another test's installed
// value would be a data race under -race.
func withSavedResolver(t *testing.T) {
	t.Helper()
	saved := arbOSPrecompileResolver
	t.Cleanup(func() { arbOSPrecompileResolver = saved })
}

// A nil f would silently disable every Arbitrum precompile on
// subsequent dispatch, so the setter must panic.
func TestSetArbOSPrecompileResolverPanicsOnNil(t *testing.T) {
	withSavedResolver(t)
	arbOSPrecompileResolver = nil

	defer func() {
		if r := recover(); r == nil {
			t.Error("SetArbOSPrecompileResolver(nil) should panic")
		}
	}()
	SetArbOSPrecompileResolver(nil)
}

// A second install would let a rogue package steal gethhook's
// resolver, so the setter must panic.
func TestSetArbOSPrecompileResolverPanicsOnDoubleInstall(t *testing.T) {
	withSavedResolver(t)
	arbOSPrecompileResolver = nil

	noop := func(arbOSVersion uint64) (PrecompiledContracts, []common.Address) {
		return nil, nil
	}
	SetArbOSPrecompileResolver(noop)

	defer func() {
		if r := recover(); r == nil {
			t.Error("second SetArbOSPrecompileResolver call should panic")
		}
	}()
	SetArbOSPrecompileResolver(noop)
}

// A non-Arbitrum chain must not consult the resolver and must use
// the Ethereum precompile switch, even when a resolver is installed.
func TestArbOSActivePrecompilesNonArbitrumPassthrough(t *testing.T) {
	withSavedResolver(t)
	arbOSPrecompileResolver = nil
	SetArbOSPrecompileResolver(func(arbOSVersion uint64) (PrecompiledContracts, []common.Address) {
		t.Error("resolver must not be called for non-Arbitrum rules")
		return nil, nil
	})

	if _, _, ok := arbOSActivePrecompiles(params.Rules{IsArbitrum: false, IsCancun: true}); ok {
		t.Error("arbOSActivePrecompiles returned ok=true for non-Arbitrum rules")
	}

	// Integration: the top-level dispatch routes to the Ethereum
	// switch, so a Cancun-configured non-Arbitrum chain yields the
	// Cancun precompile set.
	gotMap := activePrecompiledContracts(params.Rules{IsArbitrum: false, IsCancun: true})
	if len(gotMap) != len(PrecompiledContractsCancun) {
		t.Errorf("activePrecompiledContracts returned %d entries, want %d (Cancun)", len(gotMap), len(PrecompiledContractsCancun))
	}
	gotAddrs := ActivePrecompiles(params.Rules{IsArbitrum: false, IsCancun: true})
	if len(gotAddrs) != len(PrecompiledAddressesCancun) {
		t.Errorf("ActivePrecompiles returned %d entries, want %d (Cancun)", len(gotAddrs), len(PrecompiledAddressesCancun))
	}
}

// Without an installed resolver, dispatch falls through to the
// Ethereum switch so go-ethereum-as-library / core/vm-only tests
// still produce a sensible precompile set.
func TestArbOSActivePrecompilesNoResolverPassthrough(t *testing.T) {
	withSavedResolver(t)
	arbOSPrecompileResolver = nil

	if _, _, ok := arbOSActivePrecompiles(params.Rules{IsArbitrum: true, ArbOSVersion: 30}); ok {
		t.Error("arbOSActivePrecompiles returned ok=true with no resolver installed")
	}
}

// The resolver receives the exact ArbOSVersion from rules and its
// return values are forwarded unchanged through both top-level
// dispatch entry points.
func TestArbOSActivePrecompilesResolverInvoked(t *testing.T) {
	withSavedResolver(t)
	arbOSPrecompileResolver = nil

	sentinel := common.BytesToAddress([]byte{0xAB, 0xCD})
	wantContracts := PrecompiledContracts{sentinel: nil}
	wantAddrs := []common.Address{sentinel}
	var gotVersion uint64
	var callCount int
	SetArbOSPrecompileResolver(func(arbOSVersion uint64) (PrecompiledContracts, []common.Address) {
		callCount++
		gotVersion = arbOSVersion
		return wantContracts, wantAddrs
	})

	m, a, ok := arbOSActivePrecompiles(params.Rules{IsArbitrum: true, ArbOSVersion: 42})
	if !ok {
		t.Fatal("arbOSActivePrecompiles returned ok=false with resolver installed")
	}
	if gotVersion != 42 {
		t.Errorf("resolver saw ArbOSVersion=%d, want 42", gotVersion)
	}
	if _, present := m[sentinel]; !present {
		t.Error("resolver map not forwarded through arbOSActivePrecompiles")
	}
	if len(a) != 1 || a[0] != sentinel {
		t.Errorf("resolver address slice not forwarded: got %v", a)
	}

	// Integration through the two upstream entry points. Both must
	// read the resolver's result, not fall through to the Ethereum
	// switch (which would yield Cancun here).
	gotMap := activePrecompiledContracts(params.Rules{IsArbitrum: true, IsCancun: true, ArbOSVersion: 42})
	if _, present := gotMap[sentinel]; !present {
		t.Error("activePrecompiledContracts did not route to resolver map")
	}
	if len(gotMap) != 1 {
		t.Errorf("activePrecompiledContracts leaked Cancun entries: got %d, want 1", len(gotMap))
	}
	gotAddrs := ActivePrecompiles(params.Rules{IsArbitrum: true, IsCancun: true, ArbOSVersion: 42})
	if len(gotAddrs) != 1 || gotAddrs[0] != sentinel {
		t.Errorf("ActivePrecompiles did not route to resolver slice: got %v", gotAddrs)
	}

	if callCount < 1 {
		t.Error("resolver was never invoked")
	}
}

// Pre-activation invariant: when the resolver returns (nil, nil),
// dispatch must still return ok=true so that neither entry point
// falls through to the Ethereum switch. Falling through would warm
// precompile addresses the canonical Arbitrum chain treats as cold
// accounts, diverging on state root during replay.
func TestArbOSActivePrecompilesNilViewsDoNotFallThrough(t *testing.T) {
	withSavedResolver(t)
	arbOSPrecompileResolver = nil
	SetArbOSPrecompileResolver(func(arbOSVersion uint64) (PrecompiledContracts, []common.Address) {
		return nil, nil
	})

	m, a, ok := arbOSActivePrecompiles(params.Rules{IsArbitrum: true, ArbOSVersion: 0})
	if !ok {
		t.Fatal("arbOSActivePrecompiles returned ok=false for nil resolver views; pre-activation blocks would fall through")
	}
	if m != nil || a != nil {
		t.Errorf("nil resolver views were rewritten: m=%v a=%v", m, a)
	}

	// Cancun rules below must not leak through.
	if got := activePrecompiledContracts(params.Rules{IsArbitrum: true, IsCancun: true, ArbOSVersion: 0}); got != nil {
		t.Errorf("activePrecompiledContracts fell through to Cancun map (size %d)", len(got))
	}
	if got := ActivePrecompiles(params.Rules{IsArbitrum: true, IsCancun: true, ArbOSVersion: 0}); got != nil {
		t.Errorf("ActivePrecompiles fell through to Cancun slice: %v", got)
	}
}
