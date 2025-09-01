// Copyright 2024 The go-ethereum Authors
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
	gomath "math"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/params"
)

func gasSStore4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	gas := evm.AccessEvents.SlotGas(contract.Address(), stack.peek().Bytes32(), true, contract.Gas, true)
	return multigas.StorageAccessGas(gas), nil
}

func gasSLoad4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	gas := evm.AccessEvents.SlotGas(contract.Address(), stack.peek().Bytes32(), false, contract.Gas, true)
	return multigas.StorageAccessGas(gas), nil
}

func gasBalance4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	address := stack.peek().Bytes20()
	gas := evm.AccessEvents.BasicDataGas(address, false, contract.Gas, true)
	// Account lookup -> storage-access gas
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	return multigas.StorageAccessGas(gas), nil
}

func gasExtCodeSize4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	address := stack.peek().Bytes20()
	if _, isPrecompile := evm.precompile(address); isPrecompile {
		return multigas.ZeroGas(), nil
	}
	gas := evm.AccessEvents.BasicDataGas(address, false, contract.Gas, true)
	// Account lookup -> storage-access gas
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	return multigas.StorageAccessGas(gas), nil
}

func gasExtCodeHash4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	address := stack.peek().Bytes20()
	if _, isPrecompile := evm.precompile(address); isPrecompile {
		return multigas.ZeroGas(), nil
	}
	gas := evm.AccessEvents.CodeHashGas(address, false, contract.Gas, true)
	// Account lookup -> storage-access gas
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	return multigas.StorageAccessGas(gas), nil
}

func makeCallVariantGasEIP4762(oldCalculator gasFunc, withTransferCosts bool) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
		var (
			target           = common.Address(stack.Back(1).Bytes20())
			witnessGas       uint64
			_, isPrecompile  = evm.precompile(target)
			isSystemContract = target == params.HistoryStorageAddress
		)

		// If value is transferred, it is charged before 1/64th
		// is subtracted from the available gas pool.
		if withTransferCosts && !stack.Back(2).IsZero() {
			wantedValueTransferWitnessGas := evm.AccessEvents.ValueTransferGas(contract.Address(), target, contract.Gas)
			if wantedValueTransferWitnessGas > contract.Gas {
				return multigas.StorageAccessGas(wantedValueTransferWitnessGas), nil
			}
			witnessGas = wantedValueTransferWitnessGas
		} else if isPrecompile || isSystemContract {
			witnessGas = params.WarmStorageReadCostEIP2929
		} else {
			// The charging for the value transfer is done BEFORE subtracting
			// the 1/64th gas, as this is considered part of the CALL instruction.
			// (so before we get to this point)
			// But the message call is part of the subcall, for which only 63/64th
			// of the gas should be available.
			wantedMessageCallWitnessGas := evm.AccessEvents.MessageCallGas(target, contract.Gas-witnessGas)
			var overflow bool
			if witnessGas, overflow = math.SafeAdd(witnessGas, wantedMessageCallWitnessGas); overflow {
				return multigas.ZeroGas(), ErrGasUintOverflow
			}
			if witnessGas > contract.Gas {
				return multigas.StorageAccessGas(witnessGas), nil
			}
		}

		contract.Gas -= witnessGas
		// if the operation fails, adds witness gas to the gas before returning the error
		multiGas, err := oldCalculator(evm, contract, stack, mem, memorySize)
		contract.Gas += witnessGas // restore witness gas so that it can be charged at the callsite
		// Witness gas considered as storage access.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if overflow := multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, witnessGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		return multiGas, err
	}
}

var (
	gasCallEIP4762         = makeCallVariantGasEIP4762(gasCall, true)
	gasCallCodeEIP4762     = makeCallVariantGasEIP4762(gasCallCode, false)
	gasStaticCallEIP4762   = makeCallVariantGasEIP4762(gasStaticCall, false)
	gasDelegateCallEIP4762 = makeCallVariantGasEIP4762(gasDelegateCall, false)
)

func gasSelfdestructEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	beneficiaryAddr := common.Address(stack.peek().Bytes20())
	if _, isPrecompile := evm.precompile(beneficiaryAddr); isPrecompile {
		return multigas.ZeroGas(), nil
	}
	if contract.IsSystemCall {
		return multigas.ZeroGas(), nil
	}
	contractAddr := contract.Address()
	wanted := evm.AccessEvents.BasicDataGas(contractAddr, false, contract.Gas, false)
	if wanted > contract.Gas {
		return multigas.StorageAccessGas(wanted), nil
	}
	statelessGas := wanted
	balanceIsZero := evm.StateDB.GetBalance(contractAddr).Sign() == 0
	_, isPrecompile := evm.precompile(beneficiaryAddr)
	isSystemContract := beneficiaryAddr == params.HistoryStorageAddress

	if (isPrecompile || isSystemContract) && balanceIsZero {
		return multigas.StorageAccessGas(statelessGas), nil
	}

	if contractAddr != beneficiaryAddr {
		wanted := evm.AccessEvents.BasicDataGas(beneficiaryAddr, false, contract.Gas-statelessGas, false)
		if wanted > contract.Gas-statelessGas {
			return multigas.StorageAccessGas(statelessGas + wanted), nil
		}
		statelessGas += wanted
	}
	// Charge write costs if it transfers value
	if !balanceIsZero {
		wanted := evm.AccessEvents.BasicDataGas(contractAddr, true, contract.Gas-statelessGas, false)
		if wanted > contract.Gas-statelessGas {
			return multigas.StorageAccessGas(statelessGas + wanted), nil
		}
		statelessGas += wanted

		if contractAddr != beneficiaryAddr {
			if evm.StateDB.Exist(beneficiaryAddr) {
				wanted = evm.AccessEvents.BasicDataGas(beneficiaryAddr, true, contract.Gas-statelessGas, false)
			} else {
				wanted = evm.AccessEvents.AddAccount(beneficiaryAddr, true, contract.Gas-statelessGas)
			}
			if wanted > contract.Gas-statelessGas {
				return multigas.StorageAccessGas(statelessGas + wanted), nil
			}
			statelessGas += wanted
		}
	}
	// Value transfer considered as storage access.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	return multigas.StorageAccessGas(statelessGas), nil
}

func gasCodeCopyEip4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	multiGas, err := gasCodeCopy(evm, contract, stack, mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	if !contract.IsDeployment && !contract.IsSystemCall {
		var (
			codeOffset = stack.Back(1)
			length     = stack.Back(2)
		)
		uint64CodeOffset, overflow := codeOffset.Uint64WithOverflow()
		if overflow {
			uint64CodeOffset = gomath.MaxUint64
		}

		_, copyOffset, nonPaddedCopyLength := getDataAndAdjustedBounds(contract.Code, uint64CodeOffset, length.Uint64())
		// CODECOPY in EIP-4762: reading code chunks is an account state access.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		singleGas := multiGas.SingleGas()
		_, wanted := evm.AccessEvents.CodeChunksRangeGas(contract.Address(), copyOffset, nonPaddedCopyLength, uint64(len(contract.Code)), false, contract.Gas-singleGas)
		multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, wanted)
	}
	return multiGas, nil
}

func gasExtCodeCopyEIP4762(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	// memory expansion first (dynamic part of pre-2929 implementation)
	multiGas, err := gasExtCodeCopy(evm, contract, stack, mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	addr := common.Address(stack.peek().Bytes20())
	_, isPrecompile := evm.precompile(addr)
	if isPrecompile || addr == params.HistoryStorageAddress {
		// Accessing precompile or history contract is treated as a warm state read.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if overflow := multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.WarmStorageReadCostEIP2929); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		return multiGas, nil
	}
	singleGas := multiGas.SingleGas()
	wgas := evm.AccessEvents.BasicDataGas(addr, false, contract.Gas-singleGas, true)
	// General EXTCODECOPY case: account lookup → state read.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow := multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, wgas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}
