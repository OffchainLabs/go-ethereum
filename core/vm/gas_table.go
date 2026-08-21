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

package vm

import (
	"errors"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/params"
)

// memoryGasCost calculates the quadratic gas for memory expansion. It does so
// only for the memory region that is expanded, not the total memory.
func memoryGasCost(mem *Memory, newMemSize uint64) (multigas.MultiGas, error) {
	if newMemSize == 0 {
		return multigas.ZeroGas(), nil
	}
	// The maximum that will fit in a uint64 is max_word_count - 1. Anything above
	// that will result in an overflow. Additionally, a newMemSize which results in
	// a newMemSizeWords larger than 0xFFFFFFFF will cause the square operation to
	// overflow. The constant 0x1FFFFFFFE0 is the highest number that can be used
	// without overflowing the gas calculation.
	if newMemSize > 0x1FFFFFFFE0 {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	newMemSizeWords := toWordSize(newMemSize)
	newMemSize = newMemSizeWords * 32

	if newMemSize > uint64(mem.Len()) {
		square := newMemSizeWords * newMemSizeWords
		linCoef := newMemSizeWords * params.MemoryGas
		quadCoef := square / params.QuadCoeffDiv
		newTotalFee := linCoef + quadCoef

		fee := newTotalFee - mem.lastGasCost
		mem.lastGasCost = newTotalFee

		// Memory expansion considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		return multigas.ComputationGas(fee), nil
	}
	return multigas.ZeroGas(), nil
}

// memoryCopierGas creates the gas functions for the following opcodes, and takes
// the stack position of the operand which determines the size of the data to copy
// as argument:
// CALLDATACOPY (stack position 2)
// CODECOPY (stack position 2)
// MCOPY (stack position 2)
// EXTCODECOPY (stack position 3)
// RETURNDATACOPY (stack position 2)
func memoryCopierGas(stackpos int) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
		// Gas for expanding the memory
		multiGas, err := memoryGasCost(mem, memorySize)
		if err != nil {
			return multigas.ZeroGas(), err
		}
		// And gas for copying data, charged per word at param.CopyGas
		words, overflow := stack.back(stackpos).Uint64WithOverflow()
		if overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		var wordCopyGas uint64
		if wordCopyGas, overflow = math.SafeMul(toWordSize(words), params.CopyGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Distribute copy gas by dimension:
		// - For EXTCODECOPY: count as ResourceKindStorageAccessRead since it is the only opcode
		// using stack position 3 and reading from the state trie.
		// - For others: count as ResourceKindComputation since they are in-memory operations
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		var dim multigas.ResourceKind
		if stackpos == 3 {
			dim = multigas.ResourceKindStorageAccessRead // EXTCODECOPY
		} else {
			dim = multigas.ResourceKindComputation // CALLDATACOPY, CODECOPY, MCOPY, RETURNDATACOPY
		}
		if multiGas, overflow = multiGas.SafeIncrement(dim, wordCopyGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		return multiGas, nil
	}
}

var (
	gasCallDataCopy   = memoryCopierGas(2)
	gasCodeCopy       = memoryCopierGas(2)
	gasMcopy          = memoryCopierGas(2)
	gasExtCodeCopy    = memoryCopierGas(3)
	gasReturnDataCopy = memoryCopierGas(2)
)

func gasSStore(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	// Arbitrum: gate the early error return on Amsterdam fork
	isArbitrumPreAmsterdam := evm.chainRules.IsArbitrum && !evm.chainRules.IsAmsterdam
	if !isArbitrumPreAmsterdam && evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	var (
		y, x              = stack.back(1), stack.back(0)
		current, original = evm.StateDB.GetStateAndCommittedState(contract.Address(), x.Bytes32())
	)
	// The legacy gas metering only takes into consideration the current state
	// Legacy rules should be applied if we are in Petersburg (removal of EIP-1283)
	// OR Constantinople is not active
	if evm.chainRules.IsPetersburg || !evm.chainRules.IsConstantinople {
		// This checks for 3 scenarios and calculates gas accordingly:
		//
		// 1. From a zero-value address to a non-zero value         (NEW VALUE)
		// 2. From a non-zero value address to a zero-value address (DELETE)
		// 3. From a non-zero to a non-zero                         (CHANGE)
		switch {
		case current == (common.Hash{}) && y.Sign() != 0: // 0 => non 0
			return multigas.StorageGrowthGas(params.SstoreSetGas), nil
		case current != (common.Hash{}) && y.Sign() == 0: // non 0 => 0
			evm.StateDB.AddRefund(params.SstoreRefundGas)
			return multigas.StorageAccessWriteGas(params.SstoreClearGas), nil
		default: // non 0 => non 0 (or 0 => 0)
			return multigas.StorageAccessWriteGas(params.SstoreResetGas), nil
		}
	}

	// The new gas metering is based on net gas costs (EIP-1283):
	//
	// (1.) If current value equals new value (this is a no-op), 200 gas is deducted.
	// (2.) If current value does not equal new value
	//	(2.1.) If original value equals current value (this storage slot has not been changed by the current execution context)
	//		(2.1.1.) If original value is 0, 20000 gas is deducted.
	//		(2.1.2.) Otherwise, 5000 gas is deducted. If new value is 0, add 15000 gas to refund counter.
	//	(2.2.) If original value does not equal current value (this storage slot is dirty), 200 gas is deducted. Apply both of the following clauses.
	//		(2.2.1.) If original value is not 0
	//			(2.2.1.1.) If current value is 0 (also means that new value is not 0), remove 15000 gas from refund counter. We can prove that refund counter will never go below 0.
	//			(2.2.1.2.) If new value is 0 (also means that current value is not 0), add 15000 gas to refund counter.
	//		(2.2.2.) If original value equals new value (this storage slot is reset)
	//			(2.2.2.1.) If original value is 0, add 19800 gas to refund counter.
	//			(2.2.2.2.) Otherwise, add 4800 gas to refund counter.
	value := common.Hash(y.Bytes32())
	if current == value { // noop (1)
		return multigas.StorageAccessReadGas(params.NetSstoreNoopGas), nil
	}
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return multigas.StorageGrowthGas(params.NetSstoreInitGas), nil
		}

		if value == (common.Hash{}) { // delete slot (2.1.2b)
			evm.StateDB.AddRefund(params.NetSstoreClearRefund)
		}
		return multigas.StorageAccessWriteGas(params.NetSstoreCleanGas), nil // write existing slot (2.1.2)
	}
	if original != (common.Hash{}) {
		if current == (common.Hash{}) { // recreate slot (2.2.1.1)
			evm.StateDB.SubRefund(params.NetSstoreClearRefund)
		} else if value == (common.Hash{}) { // delete slot (2.2.1.2)
			evm.StateDB.AddRefund(params.NetSstoreClearRefund)
		}
	}
	if original == value {
		if original == (common.Hash{}) { // reset to original inexistent slot (2.2.2.1)
			evm.StateDB.AddRefund(params.NetSstoreResetClearRefund)
		} else { // reset to original existing slot (2.2.2.2)
			evm.StateDB.AddRefund(params.NetSstoreResetRefund)
		}
	}
	return multigas.StorageAccessWriteGas(params.NetSstoreDirtyGas), nil
}

// Here come the EIP2200 rules:
//
//	(0.) If *gasleft* is less than or equal to 2300, fail the current call.
//	(1.) If current value equals new value (this is a no-op), SLOAD_GAS is deducted.
//	(2.) If current value does not equal new value:
//		(2.1.) If original value equals current value (this storage slot has not been changed by the current execution context):
//			(2.1.1.) If original value is 0, SSTORE_SET_GAS (20K) gas is deducted.
//			(2.1.2.) Otherwise, SSTORE_RESET_GAS gas is deducted. If new value is 0, add SSTORE_CLEARS_SCHEDULE to refund counter.
//		(2.2.) If original value does not equal current value (this storage slot is dirty), SLOAD_GAS gas is deducted. Apply both of the following clauses:
//			(2.2.1.) If original value is not 0:
//				(2.2.1.1.) If current value is 0 (also means that new value is not 0), subtract SSTORE_CLEARS_SCHEDULE gas from refund counter.
//				(2.2.1.2.) If new value is 0 (also means that current value is not 0), add SSTORE_CLEARS_SCHEDULE gas to refund counter.
//			(2.2.2.) If original value equals new value (this storage slot is reset):
//				(2.2.2.1.) If original value is 0, add SSTORE_SET_GAS - SLOAD_GAS to refund counter.
//				(2.2.2.2.) Otherwise, add SSTORE_RESET_GAS - SLOAD_GAS gas to refund counter.
func gasSStoreEIP2200(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	// Arbitrum: gate the early error return on Amsterdam fork
	isArbitrumPreAmsterdam := evm.chainRules.IsArbitrum && !evm.chainRules.IsAmsterdam
	if !isArbitrumPreAmsterdam && evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	// If we fail the minimum gas availability invariant, fail (0)
	if contract.Gas.RegularGas <= params.SstoreSentryGasEIP2200 {
		return multigas.ZeroGas(), errors.New("not enough gas for reentrancy sentry")
	}
	// Gas sentry honoured, do the actual gas calculation based on the stored value
	var (
		y, x              = stack.back(1), stack.back(0)
		current, original = evm.StateDB.GetStateAndCommittedState(contract.Address(), x.Bytes32())
	)
	value := common.Hash(y.Bytes32())

	if current == value { // noop (1)
		return multigas.StorageAccessReadGas(params.SloadGasEIP2200), nil
	}
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return multigas.StorageGrowthGas(params.SstoreSetGasEIP2200), nil
		}
		if value == (common.Hash{}) { // delete slot (2.1.2b)
			evm.StateDB.AddRefund(params.SstoreClearsScheduleRefundEIP2200)
		}
		return multigas.StorageAccessWriteGas(params.SstoreResetGasEIP2200), nil
	}
	if original != (common.Hash{}) {
		if current == (common.Hash{}) { // recreate slot (2.2.1.1)
			evm.StateDB.SubRefund(params.SstoreClearsScheduleRefundEIP2200)
		} else if value == (common.Hash{}) { // delete slot (2.2.1.2)
			evm.StateDB.AddRefund(params.SstoreClearsScheduleRefundEIP2200)
		}
	}
	if original == value {
		if original == (common.Hash{}) { // reset to original inexistent slot (2.2.2.1)
			evm.StateDB.AddRefund(params.SstoreSetGasEIP2200 - params.SloadGasEIP2200)
		} else { // reset to original existing slot (2.2.2.2)
			evm.StateDB.AddRefund(params.SstoreResetGasEIP2200 - params.SloadGasEIP2200)
		}
	}
	return multigas.StorageAccessWriteGas(params.SloadGasEIP2200), nil // dirty update (2.2)
}

func makeGasLog(n uint64) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
		requestedSize, overflow := stack.back(1).Uint64WithOverflow()
		if overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		multiGas, err := memoryGasCost(mem, memorySize)
		if err != nil {
			return multigas.ZeroGas(), err
		}

		// Base LOG operation considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, params.LogGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Per-topic cost is split between history growth and computation:
		// - A fixed number of bytes per topic is persisted in history (topicBytes),
		//   and those bytes are charged at LogDataGas (gas per byte) as history growth.
		// - The remainder of the per-topic cost is attributed to computation (e.g. hashing/bloom work).

		// Scale by number of topics for LOG0..LOG4
		var topicHistTotal, topicCompTotal uint64
		if topicHistTotal, overflow = math.SafeMul(n, params.LogTopicHistoryGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		if topicCompTotal, overflow = math.SafeMul(n, params.LogTopicComputationGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Apply the split.
		if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindHistoryGrowth, topicHistTotal); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, topicCompTotal); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Data payload bytes → history growth at LogDataGas (gas per byte).
		var dataHist uint64
		if dataHist, overflow = math.SafeMul(requestedSize, params.LogDataGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		// Event log data considered as history growth.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindHistoryGrowth, dataHist); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		return multiGas, nil
	}
}

func gasKeccak256(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	wordGas, overflow := stack.back(1).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if wordGas, overflow = math.SafeMul(toWordSize(wordGas), params.Keccak256WordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, wordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

// pureMemoryGascost is used by several operations, which aside from their
// static cost have a dynamic cost which is solely based on the memory
// expansion
func pureMemoryGascost(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	return memoryGasCost(mem, memorySize)
}

var (
	gasReturn  = pureMemoryGascost
	gasRevert  = pureMemoryGascost
	gasMLoad   = pureMemoryGascost
	gasMStore8 = pureMemoryGascost
	gasMStore  = pureMemoryGascost
)

func gasCreate(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	if evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	return pureMemoryGascost(evm, contract, stack, mem, memorySize)
}

func gasCreate2(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	if evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	wordGas, overflow := stack.back(2).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if wordGas, overflow = math.SafeMul(toWordSize(wordGas), params.Keccak256WordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	// Keccak hashing considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, wordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasCreateEip3860(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	if evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	size, overflow := stack.back(2).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if err := CheckMaxInitCodeSize(&evm.chainRules, size); err != nil {
		return multigas.ZeroGas(), err
	}
	// Since size <= the protocol-defined maximum initcode size limit, these multiplication cannot overflow
	moreGas := params.InitCodeWordGas * ((size + 31) / 32)

	// Init code execution considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, moreGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasCreate2Eip3860(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	if evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	size, overflow := stack.back(2).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if err := CheckMaxInitCodeSize(&evm.chainRules, size); err != nil {
		return multigas.ZeroGas(), err
	}
	// Since size <= the protocol-defined maximum initcode size limit, these multiplication cannot overflow
	moreGas := (params.InitCodeWordGas + params.Keccak256WordGas) * ((size + 31) / 32)

	// Init code execution and Keccak hashing both considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, moreGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasExpFrontier(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	expByteLen := uint64((stack.back(1).BitLen() + 7) / 8)

	var (
		gas      = expByteLen * params.ExpByteFrontier // no overflow check required. Max is 256 * ExpByte gas
		overflow bool
	)
	if gas, overflow = math.SafeAdd(gas, params.ExpGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multigas.ComputationGas(gas), nil
}

func gasExpEIP158(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	expByteLen := uint64((stack.back(1).BitLen() + 7) / 8)

	var (
		gas      = expByteLen * params.ExpByteEIP158 // no overflow check required. Max is 256 * ExpByte gas
		overflow bool
	)
	if gas, overflow = math.SafeAdd(gas, params.ExpGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multigas.ComputationGas(gas), nil
}

var (
	gasCall         = makeCallVariantGasCost(gasCallIntrinsic)
	gasCallCode     = makeCallVariantGasCost(gasCallCodeIntrinsic)
	gasDelegateCall = makeCallVariantGasCost(gasDelegateCallIntrinsic)
	gasStaticCall   = makeCallVariantGasCost(gasStaticCallIntrinsic)
)

func makeCallVariantGasCost(intrinsicFunc gasFunc) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
		multiIntrinsic, err := intrinsicFunc(evm, contract, stack, mem, memorySize)
		if err != nil {
			return multigas.ZeroGas(), err
		}
		intrinsic := multiIntrinsic.SingleGas()
		evm.callGasTemp, err = callGas(evm.chainRules.IsEIP150, contract.Gas.RegularGas, intrinsic, stack.back(0))
		if err != nil {
			return multigas.ZeroGas(), err
		}
		// Call gas forwarding considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		var overflow bool
		multiIntrinsic, overflow = multiIntrinsic.SafeIncrement(multigas.ResourceKindComputation, evm.callGasTemp)
		if overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		return multiIntrinsic, nil
	}
}

func gasCallIntrinsic(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	var (
		multiGas               = multigas.ZeroGas()
		transfersValue         = !stack.back(2).IsZero()
		address                = common.Address(stack.back(1).Bytes20())
		isArbitrumPreAmsterdam = evm.chainRules.IsArbitrum && !evm.chainRules.IsAmsterdam
	)

	// Arbitrum: gate the early error return on Amsterdam fork
	if !isArbitrumPreAmsterdam && evm.readOnly && transfersValue {
		return multigas.ZeroGas(), ErrWriteProtection
	}

	// Stateless check
	memoryMultiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		// Arbitrum: before Amsterdam memoryGasCost was called after checking account existence/emptiness;
		// to keep block recording backward compatibility we execute the state reading check before returning
		if isArbitrumPreAmsterdam && (!evm.chainRules.IsEIP158 || transfersValue) {
			// both StateDB.Empty and StateDB.Exist call StateDB.getStateObject that we care about, from state read perspective it doesn't matter which one we call
			_ = evm.StateDB.Exist(address)
		}
		return multigas.ZeroGas(), err
	}
	if transfersValue && !evm.chainRules.IsEIP4762 {
		// Value transfer to non-empty account considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindComputation, params.CallValueTransferGas)
	}
	var overflow bool
	if multiGas, overflow = multiGas.SafeAdd(memoryMultiGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	// Terminate the gas measurement if the leftover gas is not sufficient,
	// it can effectively prevent accessing the states in the following steps.
	// Arbitrum: gate the early error return on Amsterdam fork
	if !isArbitrumPreAmsterdam && contract.Gas.RegularGas < multiGas.SingleGas() {
		return multigas.ZeroGas(), ErrOutOfGas
	}
	// Stateful check
	var stateGas uint64
	if evm.chainRules.IsEIP158 {
		if transfersValue && evm.StateDB.Empty(address) {
			stateGas += params.CallNewAccountGas
		}
	} else if !evm.StateDB.Exist(address) {
		stateGas += params.CallNewAccountGas
	}
	// Storage slot writes (zero → nonzero) considered as storage growth.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if multiGas, overflow = multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, stateGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasCallCodeIntrinsic(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	memoryMultiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	var (
		multiGas = multigas.ZeroGas()
		overflow bool
	)
	if stack.back(2).Sign() != 0 && !evm.chainRules.IsEIP4762 {
		// Value transfer to non-empty account considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindComputation, params.CallValueTransferGas)
	}
	multiGas, overflow = multiGas.SafeAdd(memoryMultiGas)
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasDelegateCallIntrinsic(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	return memoryGasCost(mem, memorySize)
}

func gasStaticCallIntrinsic(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	return memoryGasCost(mem, memorySize)
}

func gasSelfdestruct(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	// Arbitrum: gate the early error return on Amsterdam fork
	isArbitrumPreAmsterdam := evm.chainRules.IsArbitrum && !evm.chainRules.IsAmsterdam
	if !isArbitrumPreAmsterdam && evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	multiGas := multigas.ZeroGas()
	// EIP150 homestead gas reprice fork:
	if evm.chainRules.IsEIP150 {
		// SELFDESTRUCT base cost covers deleting the contract from state — a destructive write.
		// This predates EIP-2929, so it doesn't decompose into cold/warm phases; the full cost
		// is attributed to StorageAccessWrite.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindStorageAccessWrite, params.SelfdestructGasEIP150)
		var address = common.Address(stack.back(0).Bytes20())

		// New account creation considered as storage growth.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if evm.chainRules.IsEIP158 {
			// if empty and transfers value
			if evm.StateDB.Empty(address) && evm.StateDB.GetBalance(contract.Address()).Sign() != 0 {
				multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
			}
		} else if !evm.StateDB.Exist(address) {
			multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
		}
	}

	if !evm.StateDB.HasSelfDestructed(contract.Address()) {
		evm.StateDB.AddRefund(params.SelfdestructRefundGas)
	}
	return multiGas, nil
}

func gasSelfdestruct8037(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	if evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	var (
		multiGas = multigas.ZeroGas()
		address  = common.Address(stack.peek().Bytes20())
	)
	if !evm.StateDB.AddressInAccessList(address) {
		// If the caller cannot afford the cost, this change will be rolled back
		evm.StateDB.AddAddressToAccessList(address)
		multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindStorageAccessRead, params.ColdAccountAccessCostEIP2929)
	}
	// Check we have enough regular gas before we add the address to the BAL
	if contract.Gas.RegularGas < multiGas.SingleGas() {
		return multiGas, ErrOutOfGas
	}
	// Important: use StateDB.Empty instead of !StateDB.Exist. An account may exist
	// in the current state yet still be considered non-existent by EIP-161 if its
	// nonce, balance, and code are all zero. Such accounts can appear temporarily
	// during execution (e.g. via SELFDESTRUCT) and are removed at tx end.
	//
	// Funding such an account makes it permanent state growth and must be charged.
	if evm.StateDB.Empty(address) && evm.StateDB.GetBalance(contract.Address()).Sign() != 0 {
		multiGas = multiGas.WithStateGas(params.AccountCreationSize * evm.Context.CostPerStateByte)
	}
	return multiGas, nil
}

func gasSStore8037(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (multigas.MultiGas, error) {
	if evm.readOnly {
		return multigas.ZeroGas(), ErrWriteProtection
	}
	// If we fail the minimum gas availability invariant, fail (0)
	if contract.Gas.RegularGas <= params.SstoreSentryGasEIP2200 {
		return multigas.ZeroGas(), errors.New("not enough gas for reentrancy sentry")
	}
	// Gas sentry honoured, do the actual gas calculation based on the stored value
	var (
		y, x              = stack.back(1), stack.peek()
		slot              = common.Hash(x.Bytes32())
		current, original = evm.StateDB.GetStateAndCommittedState(contract.Address(), slot)
		multiGas          = multigas.ZeroGas()
	)
	// Check slot presence in the access list
	if _, slotPresent := evm.StateDB.SlotInAccessList(contract.Address(), slot); !slotPresent {
		multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindStorageAccessRead, params.ColdSloadCostEIP2929)
		// If the caller cannot afford the cost, this change will be rolled back
		evm.StateDB.AddSlotToAccessList(contract.Address(), slot)
	}
	value := common.Hash(y.Bytes32())

	if current == value { // noop (1)
		// EIP 2200 original clause:
		//		return params.SloadGasEIP2200, nil
		return multiGas.SaturatingIncrement(multigas.ResourceKindComputation, params.WarmStorageReadCostEIP2929), nil // SLOAD_GAS
	}
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			multiGas = multiGas.SaturatingIncrement(multigas.ResourceKindStorageAccessWrite, params.SstoreResetGasEIP2200-params.ColdSloadCostEIP2929)
			return multiGas.WithStateGas(params.StorageCreationSize * evm.Context.CostPerStateByte), nil
		}
		if value == (common.Hash{}) { // delete slot (2.1.2b)
			evm.StateDB.AddRefund(params.SstoreClearsScheduleRefundEIP3529)
		}
		// EIP-2200 original clause:
		//		return params.SstoreResetGasEIP2200, nil // write existing slot (2.1.2)
		return multiGas.SaturatingIncrement(multigas.ResourceKindStorageAccessWrite, params.SstoreResetGasEIP2200-params.ColdSloadCostEIP2929), nil // write existing slot (2.1.2)
	}
	if original != (common.Hash{}) {
		if current == (common.Hash{}) { // recreate slot (2.2.1.1)
			evm.StateDB.SubRefund(params.SstoreClearsScheduleRefundEIP3529)
		} else if value == (common.Hash{}) { // delete slot (2.2.1.2)
			evm.StateDB.AddRefund(params.SstoreClearsScheduleRefundEIP3529)
		}
	}
	if original == value {
		if original == (common.Hash{}) { // reset to original inexistent slot (2.2.2.1)
			// EIP-8037 point (2): refund state gas directly to the reservoir
			// at the SSTORE restoration point (0→x→0 in same tx); not to the
			// refund counter, which is capped at gas_used/5.
			contract.Gas.RefundState(params.StorageCreationSize * evm.Context.CostPerStateByte)

			// Regular portion of the refund still goes through the refund counter.
			evm.StateDB.AddRefund(params.SstoreResetGasEIP2200 - params.ColdSloadCostEIP2929 - params.WarmStorageReadCostEIP2929)
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
	return multiGas.SaturatingIncrement(multigas.ResourceKindComputation, params.WarmStorageReadCostEIP2929), nil // dirty update (2.2)
}
