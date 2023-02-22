// Copyright 2020 The go-ethereum Authors
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

// Computes the cost of doing a state load as of The Merge
// Note: the code here is adapted from gasSLoadEIP2929
func StateLoadCost(db StateDB, account common.Address, key common.Hash) uint64 {
	// Check slot presence in the access list
	if _, slotPresent := db.SlotInAccessList(account, key); !slotPresent {
		// If the caller cannot afford the cost, this change will be rolled back
		// If he does afford it, we can skip checking the same thing later on, during execution
		db.AddSlotToAccessList(account, key)
		return params.ColdSloadCostEIP2929
	}
	return params.WarmStorageReadCostEIP2929
}

// Computes the cost of doing a state store as of The Merge
// Note: the code here is adapted from makeGasSStoreFunc with the most recent parameters as of The Merge
// Note: the sentry check must be done by the caller
func StateStoreCost(db StateDB, account common.Address, key, value common.Hash) uint64 {
	clearingRefund := params.SstoreClearsScheduleRefundEIP3529

	cost := uint64(0)
	current := db.GetState(account, key)

	// Check slot presence in the access list
	if addrPresent, slotPresent := db.SlotInAccessList(account, key); !slotPresent {
		cost = params.ColdSloadCostEIP2929
		// If the caller cannot afford the cost, this change will be rolled back
		db.AddSlotToAccessList(account, key)
		if !addrPresent {
			panic("impossible case: address was not present in access list")
		}
	}

	if current == value { // noop (1)
		// EIP 2200 original clause:
		//		return params.SloadGasEIP2200, nil
		return cost + params.WarmStorageReadCostEIP2929 // SLOAD_GAS
	}
	original := db.GetCommittedState(account, key)
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return cost + params.SstoreSetGasEIP2200
		}
		if value == (common.Hash{}) { // delete slot (2.1.2b)
			db.AddRefund(clearingRefund)
		}
		// EIP-2200 original clause:
		//		return params.SstoreResetGasEIP2200, nil // write existing slot (2.1.2)
		return cost + (params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) // write existing slot (2.1.2)
	}
	if original != (common.Hash{}) {
		if current == (common.Hash{}) { // recreate slot (2.2.1.1)
			db.SubRefund(clearingRefund)
		} else if value == (common.Hash{}) { // delete slot (2.2.1.2)
			db.AddRefund(clearingRefund)
		}
	}
	if original == value {
		if original == (common.Hash{}) { // reset to original inexistent slot (2.2.2.1)
			// EIP 2200 Original clause:
			//evm.StateDB.AddRefund(params.SstoreSetGasEIP2200 - params.SloadGasEIP2200)
			db.AddRefund(params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929)
		} else { // reset to original existing slot (2.2.2.2)
			// EIP 2200 Original clause:
			//	evm.StateDB.AddRefund(params.SstoreResetGasEIP2200 - params.SloadGasEIP2200)
			// - SSTORE_RESET_GAS redefined as (5000 - COLD_SLOAD_COST)
			// - SLOAD_GAS redefined as WARM_STORAGE_READ_COST
			// Final: (5000 - COLD_SLOAD_COST) - WARM_STORAGE_READ_COST
			db.AddRefund((params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929)
		}
	}
	// EIP-2200 original clause:
	//return params.SloadGasEIP2200, nil // dirty update (2.2)
	return cost + params.WarmStorageReadCostEIP2929 // dirty update (2.2)
}
