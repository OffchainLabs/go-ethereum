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
	"errors"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func makeGasSStoreFunc(clearingRefund uint64) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
		// If we fail the minimum gas availability invariant, fail (0)
		if contract.Gas <= params.SstoreSentryGasEIP2200 {
			return multigas.ZeroGas(), errors.New("not enough gas for reentrancy sentry")
		}
		// Gas sentry honoured, do the actual gas calculation based on the stored value
		var (
			y, x     = stack.Back(1), stack.peek()
			slot     = common.Hash(x.Bytes32())
			current  = evm.StateDB.GetState(contract.Address(), slot)
			multiGas = multigas.ZeroGas()
		)
		// Check slot presence in the access list
		if _, slotPresent := evm.StateDB.SlotInAccessList(contract.Address(), slot); !slotPresent {
			// Cold slot access considered as storage access.
			// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.ColdSloadCostEIP2929)

			// If the caller cannot afford the cost, this change will be rolled back
			evm.StateDB.AddSlotToAccessList(contract.Address(), slot)
		}
		value := common.Hash(y.Bytes32())

		if current == value { // noop (1)
			// EIP 2200 original clause:
			//		return params.SloadGasEIP2200, nil

			// Warm slot access considered as storage access.
			// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.WarmStorageReadCostEIP2929)
			return multiGas, nil // SLOAD_GAS
		}
		original := evm.StateDB.GetCommittedState(contract.Address(), x.Bytes32())
		if original == current {
			if original == (common.Hash{}) { // create slot (2.1.1)
				// Creating a new slot considered as storage growth.
				// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
				multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.SstoreSetGasEIP2200)
				return multiGas, nil
			}
			if value == (common.Hash{}) { // delete slot (2.1.2b)
				evm.StateDB.AddRefund(clearingRefund)
			}
			// EIP-2200 original clause:
			//		return params.SstoreResetGasEIP2200, nil // write existing slot (2.1.2)

			//  Storage slot writes (nonzero → zero) considered as storage access.
			//  See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.SstoreResetGasEIP2200-params.ColdSloadCostEIP2929)
			return multiGas, nil // write existing slot (2.1.2)
		}
		if original != (common.Hash{}) {
			if current == (common.Hash{}) { // recreate slot (2.2.1.1)
				evm.StateDB.SubRefund(clearingRefund)
			} else if value == (common.Hash{}) { // delete slot (2.2.1.2)
				evm.StateDB.AddRefund(clearingRefund)
			}
		}
		if original == value {
			if original == (common.Hash{}) { // reset to original inexistent slot (2.2.2.1)
				// EIP 2200 Original clause:
				//evm.StateDB.AddRefund(params.SstoreSetGasEIP2200 - params.SloadGasEIP2200)
				evm.StateDB.AddRefund(params.SstoreSetGasEIP2200 - params.WarmStorageReadCostEIP2929)
			} else { // reset to original existing slot (2.2.2.2)
				// EIP 2200 Original clause:
				//	evm.StateDB.AddRefund(params.SstoreResetGasEIP2200 - params.SloadGasEIP2200)
				// - SSTORE_RESET_GAS redefined as (5000 - COLD_SLOAD_COST)
				// - SLOAD_GAS redefined as WARM_STORAGE_READ_COST
				// Final: (5000 - COLD_SLOAD_COST) - WARM_STORAGE_READ_COST
				evm.StateDB.AddRefund((params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929) - params.WarmStorageReadCostEIP2929)
			}
		}
		// EIP-2200 original clause:
		//return params.SloadGasEIP2200, nil // dirty update (2.2)

		// Warm slot access considered as storage access.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.WarmStorageReadCostEIP2929)
		return multiGas, nil // dirty update (2.2)
	}
}

// gasSLoadEIP2929 calculates dynamic gas for SLOAD according to EIP-2929
// For SLOAD, if the (address, storage_key) pair (where address is the address of the contract
// whose storage is being read) is not yet in accessed_storage_keys,
// charge 2100 gas and add the pair to accessed_storage_keys.
// If the pair is already in accessed_storage_keys, charge 100 gas.
func gasSLoadEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	loc := stack.peek()
	slot := common.Hash(loc.Bytes32())
	// Check slot presence in the access list
	if _, slotPresent := evm.StateDB.SlotInAccessList(contract.Address(), slot); !slotPresent {
		// If the caller cannot afford the cost, this change will be rolled back
		// If he does afford it, we can skip checking the same thing later on, during execution
		evm.StateDB.AddSlotToAccessList(contract.Address(), slot)
		// TODO(NIT-3484): Update multi dimensional gas here
		return multigas.UnknownGas(params.ColdSloadCostEIP2929), nil
	}
	// TODO(NIT-3484): Update multi dimensional gas here
	return multigas.UnknownGas(params.WarmStorageReadCostEIP2929), nil
}

// gasExtCodeCopyEIP2929 implements extcodecopy according to EIP-2929
// EIP spec:
// > If the target is not in accessed_addresses,
// > charge COLD_ACCOUNT_ACCESS_COST gas, and add the address to accessed_addresses.
// > Otherwise, charge WARM_STORAGE_READ_COST gas.
func gasExtCodeCopyEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	// memory expansion first (dynamic part of pre-2929 implementation)
	multiGas, err := gasExtCodeCopy(evm, contract, stack, mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	addr := common.Address(stack.peek().Bytes20())
	// Check slot presence in the access list
	if !evm.StateDB.AddressInAccessList(addr) {
		evm.StateDB.AddAddressToAccessList(addr)
		var overflow bool
		// We charge (cold-warm), since 'warm' is already charged as constantGas
		// TODO(NIT-3484): Update multi dimensional gas here
		if overflow = multiGas.SafeIncrement(multigas.ResourceKindUnknown, params.ColdAccountAccessCostEIP2929-params.WarmStorageReadCostEIP2929); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		return multiGas, nil
	}
	return multiGas, nil
}

// gasEip2929AccountCheck checks whether the first stack item (as address) is present in the access list.
// If it is, this method returns '0', otherwise 'cold-warm' gas, presuming that the opcode using it
// is also using 'warm' as constant factor.
// This method is used by:
// - extcodehash,
// - extcodesize,
// - (ext) balance
func gasEip2929AccountCheck(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	addr := common.Address(stack.peek().Bytes20())
	// Check slot presence in the access list
	if !evm.StateDB.AddressInAccessList(addr) {
		// If the caller cannot afford the cost, this change will be rolled back
		evm.StateDB.AddAddressToAccessList(addr)
		// The warm storage read cost is already charged as constantGas
		// TODO(NIT-3484): Update multi dimensional gas here
		return multigas.UnknownGas(params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929), nil
	}
	return multigas.ZeroGas(), nil
}

func makeCallVariantGasCallEIP2929(oldCalculator gasFunc, addressPosition int) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
		addr := common.Address(stack.Back(addressPosition).Bytes20())
		// Check slot presence in the access list
		warmAccess := evm.StateDB.AddressInAccessList(addr)
		// The WarmStorageReadCostEIP2929 (100) is already deducted in the form of a constant cost, so
		// the cost to charge for cold access, if any, is Cold - Warm
		coldCost := params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
		if !warmAccess {
			evm.StateDB.AddAddressToAccessList(addr)
			// Charge the remaining difference here already, to correctly calculate available
			// gas for call
			if !contract.UseGas(coldCost, evm.Config.Tracer, tracing.GasChangeCallStorageColdAccess) {
				return multigas.ZeroGas(), ErrOutOfGas
			}
		}
		// Now call the old calculator, which takes into account
		// - create new account
		// - transfer value
		// - memory expansion
		// - 63/64ths rule
		multiGas, err := oldCalculator(evm, contract, stack, mem, memorySize)
		if warmAccess || err != nil {
			return multiGas, err
		}
		// In case of a cold access, we temporarily add the cold charge back, and also
		// add it to the returned gas. By adding it to the return, it will be charged
		// outside of this function, as part of the dynamic gas, and that will make it
		// also become correctly reported to tracers.
		contract.Gas += coldCost

		// Cold slot access considered as storage access.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if overflow := multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, coldCost); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		return multiGas, nil
	}
}

var (
	gasCallEIP2929         = makeCallVariantGasCallEIP2929(gasCall, 1)
	gasDelegateCallEIP2929 = makeCallVariantGasCallEIP2929(gasDelegateCall, 1)
	gasStaticCallEIP2929   = makeCallVariantGasCallEIP2929(gasStaticCall, 1)
	gasCallCodeEIP2929     = makeCallVariantGasCallEIP2929(gasCallCode, 1)
	gasSelfdestructEIP2929 = makeSelfdestructGasFn(true)
	// gasSelfdestructEIP3529 implements the changes in EIP-3529 (no refunds)
	gasSelfdestructEIP3529 = makeSelfdestructGasFn(false)

	// gasSStoreEIP2929 implements gas cost for SSTORE according to EIP-2929
	//
	// When calling SSTORE, check if the (address, storage_key) pair is in accessed_storage_keys.
	// If it is not, charge an additional COLD_SLOAD_COST gas, and add the pair to accessed_storage_keys.
	// Additionally, modify the parameters defined in EIP 2200 as follows:
	//
	// Parameter 	Old value 	New value
	// SLOAD_GAS 	800 	= WARM_STORAGE_READ_COST
	// SSTORE_RESET_GAS 	5000 	5000 - COLD_SLOAD_COST
	//
	//The other parameters defined in EIP 2200 are unchanged.
	// see gasSStoreEIP2200(...) in core/vm/gas_table.go for more info about how EIP 2200 is specified
	gasSStoreEIP2929 = makeGasSStoreFunc(params.SstoreClearsScheduleRefundEIP2200)

	// gasSStoreEIP3529 implements gas cost for SSTORE according to EIP-3529
	// Replace `SSTORE_CLEARS_SCHEDULE` with `SSTORE_RESET_GAS + ACCESS_LIST_STORAGE_KEY_COST` (4,800)
	gasSStoreEIP3529 = makeGasSStoreFunc(params.SstoreClearsScheduleRefundEIP3529)
)

// makeSelfdestructGasFn can create the selfdestruct dynamic gas function for EIP-2929 and EIP-3529
func makeSelfdestructGasFn(refundsEnabled bool) gasFunc {
	gasFunc := func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
		var (
			multiGas = multigas.ZeroGas()
			address  = common.Address(stack.peek().Bytes20())
		)
		if !evm.StateDB.AddressInAccessList(address) {
			// If the caller cannot afford the cost, this change will be rolled back
			evm.StateDB.AddAddressToAccessList(address)
			// Cold account access considered as storage access.
			// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.ColdAccountAccessCostEIP2929)
		}
		// if empty and transfers value
		if evm.StateDB.Empty(address) && evm.StateDB.GetBalance(contract.Address()).Sign() != 0 {
			// New account creation considered as storage growth.
			// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
		}
		if refundsEnabled && !evm.StateDB.HasSelfDestructed(contract.Address()) {
			evm.StateDB.AddRefund(params.SelfdestructRefundGas)
		}
		return multiGas, nil
	}
	return gasFunc
}

var (
	gasCallEIP7702         = makeCallVariantGasCallEIP7702(gasCall)
	gasDelegateCallEIP7702 = makeCallVariantGasCallEIP7702(gasDelegateCall)
	gasStaticCallEIP7702   = makeCallVariantGasCallEIP7702(gasStaticCall)
	gasCallCodeEIP7702     = makeCallVariantGasCallEIP7702(gasCallCode)
)

func makeCallVariantGasCallEIP7702(oldCalculator gasFunc) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
		var (
			multiGas = multigas.ZeroGas() // total dynamic gas used
			addr     = common.Address(stack.Back(1).Bytes20())
		)

		// Check slot presence in the access list
		if !evm.StateDB.AddressInAccessList(addr) {
			evm.StateDB.AddAddressToAccessList(addr)
			// The WarmStorageReadCostEIP2929 (100) is already deducted in the form of a constant cost, so
			// the cost to charge for cold access, if any, is Cold - Warm
			coldCost := params.ColdAccountAccessCostEIP2929 - params.WarmStorageReadCostEIP2929
			// Charge the remaining difference here already, to correctly calculate available
			// gas for call
			if !contract.UseGas(coldCost, evm.Config.Tracer, tracing.GasChangeCallStorageColdAccess) {
				return multigas.ZeroGas(), ErrOutOfGas
			}
			// Cold slot access considered as storage access.
			// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, coldCost)
		}

		// Check if code is a delegation and if so, charge for resolution.
		if target, ok := types.ParseDelegation(evm.StateDB.GetCode(addr)); ok {
			var cost uint64
			if evm.StateDB.AddressInAccessList(target) {
				cost = params.WarmStorageReadCostEIP2929
			} else {
				evm.StateDB.AddAddressToAccessList(target)
				cost = params.ColdAccountAccessCostEIP2929
			}
			if !contract.UseGas(cost, evm.Config.Tracer, tracing.GasChangeCallStorageColdAccess) {
				return multigas.ZeroGas(), ErrOutOfGas
			}

			// Target address resolution considered as storage access.
			// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
			multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, cost)
		}

		// Now call the old calculator, which takes into account
		// - create new account
		// - transfer value
		// - memory expansion
		// - 63/64ths rule
		multiOld, err := oldCalculator(evm, contract, stack, mem, memorySize)
		if err != nil {
			return multiOld, err
		}

		// Temporarily add the gas charge back to the contract and return value. By
		// adding it to the return, it will be charged outside of this function, as
		// part of the dynamic gas. This will ensure it is correctly reported to
		// tracers.
		contract.Gas += multiGas.Get(multigas.ResourceKindStorageAccess)

		var overflow bool
		if multiGas, overflow = multiGas.SafeAdd(multiGas, multiOld); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		return multiGas, nil
	}
}
