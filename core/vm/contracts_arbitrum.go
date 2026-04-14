// Copyright 2024-2026 The go-ethereum Authors
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
	"github.com/ethereum/go-ethereum/params"
)

// arbOSPrecompileResolver is installed at init by downstream
// Arbitrum integrations (Nitro's gethhook) so go-ethereum stays
// unaware of ArbOS activation-version semantics, minimizing the
// delta vs upstream ethereum/go-ethereum.
//
// Returned views must be read-only: they are typically a shared,
// cached snapshot.
var arbOSPrecompileResolver func(arbOSVersion uint64) (PrecompiledContracts, []common.Address)

// SetArbOSPrecompileResolver panics on a nil argument (so a botched
// function value can't silently revert dispatch) and on a second
// install (so a rogue downstream package can't steal gethhook's
// resolver).
func SetArbOSPrecompileResolver(f func(arbOSVersion uint64) (PrecompiledContracts, []common.Address)) {
	if f == nil {
		panic("vm.SetArbOSPrecompileResolver: nil resolver")
	}
	if arbOSPrecompileResolver != nil {
		panic("vm.SetArbOSPrecompileResolver: resolver already installed")
	}
	arbOSPrecompileResolver = f
}

// arbOSActivePrecompiles isolates the Arbitrum dispatch branch so
// the upstream call sites in contracts.go stay a short guard and
// rebases against ethereum/go-ethereum remain cheap.
//
// ok=false means "let the Ethereum switch handle it" — either a
// non-Arbitrum chain or no resolver installed (core/vm tests or
// go-ethereum-as-library without gethhook).
//
// ok=true replaces the Ethereum switch outright, even when the
// resolver returns nil views. A pre-activation Arbitrum block must
// not fall through to the Ethereum switch below: ArbOS gas semantics
// differ, and falling through would warm precompile addresses the
// canonical chain treats as cold accounts.
func arbOSActivePrecompiles(rules params.Rules) (PrecompiledContracts, []common.Address, bool) {
	if !rules.IsArbitrum || arbOSPrecompileResolver == nil {
		return nil, nil, false
	}
	m, a := arbOSPrecompileResolver(rules.ArbOSVersion)
	return m, a, true
}
