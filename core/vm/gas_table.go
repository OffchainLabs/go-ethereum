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
	"fmt"

	"github.com/ethereum/go-ethereum/arbitrum/multigas"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/params"
)

// memoryGasCost calculates the quadratic gas for memory expansion. It does so
// only for the memory region that is expanded, not the total memory.
func memoryGasCost(mem *Memory, newMemSize uint64) (*multigas.MultiGas, error) {
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
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
		// Gas for expanding the memory
		multiGas, err := memoryGasCost(mem, memorySize)
		if err != nil {
			return multigas.ZeroGas(), err
		}
		// And gas for copying data, charged per word at param.CopyGas
		words, overflow := stack.Back(stackpos).Uint64WithOverflow()
		if overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		if words, overflow = math.SafeMul(toWordSize(words), params.CopyGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// TODO(NIT-3484): Update multi dimensional gas here
		if overflow = multiGas.SafeIncrement(multigas.ResourceKindUnknown, words); overflow {
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

func gasSStore(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	var (
		y, x    = stack.Back(1), stack.Back(0)
		current = evm.StateDB.GetState(contract.Address(), x.Bytes32())
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
			return multigas.StorageAccessGas(params.SstoreClearGas), nil
		default: // non 0 => non 0 (or 0 => 0)
			return multigas.StorageAccessGas(params.SstoreResetGas), nil
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
		return multigas.StorageAccessGas(params.NetSstoreNoopGas), nil
	}
	original := evm.StateDB.GetCommittedState(contract.Address(), x.Bytes32())
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return multigas.StorageGrowthGas(params.NetSstoreInitGas), nil
		}

		if value == (common.Hash{}) { // delete slot (2.1.2b)
			evm.StateDB.AddRefund(params.NetSstoreClearRefund)
		}
		return multigas.StorageAccessGas(params.NetSstoreCleanGas), nil // write existing slot (2.1.2)
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
	return multigas.StorageAccessGas(params.NetSstoreDirtyGas), nil
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
func gasSStoreEIP2200(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	// If we fail the minimum gas availability invariant, fail (0)
	if contract.Gas <= params.SstoreSentryGasEIP2200 {
		return multigas.ZeroGas(), errors.New("not enough gas for reentrancy sentry")
	}
	// Gas sentry honoured, do the actual gas calculation based on the stored value
	var (
		y, x    = stack.Back(1), stack.Back(0)
		current = evm.StateDB.GetState(contract.Address(), x.Bytes32())
	)
	value := common.Hash(y.Bytes32())

	if current == value { // noop (1)
		return multigas.StorageAccessGas(params.SloadGasEIP2200), nil
	}
	original := evm.StateDB.GetCommittedState(contract.Address(), x.Bytes32())
	if original == current {
		if original == (common.Hash{}) { // create slot (2.1.1)
			return multigas.StorageGrowthGas(params.SstoreSetGasEIP2200), nil
		}
		if value == (common.Hash{}) { // delete slot (2.1.2b)
			evm.StateDB.AddRefund(params.SstoreClearsScheduleRefundEIP2200)
		}

		return multigas.StorageAccessGas(params.SstoreResetGasEIP2200), nil
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
	return multigas.StorageAccessGas(params.SloadGasEIP2200), nil // dirty update (2.2)
}

func makeGasLog(n uint64) gasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
		requestedSize, overflow := stack.Back(1).Uint64WithOverflow()
		if overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		multiGas, err := memoryGasCost(mem, memorySize)
		if err != nil {
			return multigas.ZeroGas(), err
		}

		// Base LOG operation considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, params.LogGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Per-topic cost is split between history growth and computation:
		// - A fixed number of bytes per topic is persisted in history (topicBytes),
		//   and those bytes are charged at LogDataGas (gas per byte) as history growth.
		// - The remainder of the per-topic cost is attributed to computation (e.g. hashing/bloom work).

		// 32 bytes per topic represents the hash size that gets stored in history.
		const topicBytes = 32

		var topicHistPer uint64
		// History growth per topic = topicBytes * LogDataGas
		if topicHistPer, overflow = math.SafeMul(topicBytes, params.LogDataGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		// Sanity check: the per-topic total must be at least the history-growth portion.
		if params.LogTopicGas < topicHistPer {
			return multigas.ZeroGas(), fmt.Errorf(
				"invalid gas params: LogTopicGas(%d) < topicBytes*LogDataGas(%d)",
				params.LogTopicGas, topicHistPer,
			)
		}
		// Computation per topic = LogTopicGas - (topicBytes * LogDataGas)
		topicCompPer := params.LogTopicGas - topicHistPer

		// Scale by number of topics for LOG0..LOG4
		var topicHistTotal, topicCompTotal uint64
		if topicHistTotal, overflow = math.SafeMul(n, topicHistPer); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		if topicCompTotal, overflow = math.SafeMul(n, topicCompPer); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Apply the split.
		if overflow = multiGas.SafeIncrement(multigas.ResourceKindHistoryGrowth, topicHistTotal); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, topicCompTotal); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}

		// Data payload bytes → history growth at LogDataGas (gas per byte).
		var dataHist uint64
		if dataHist, overflow = math.SafeMul(requestedSize, params.LogDataGas); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		// Event log data considered as history growth.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if overflow = multiGas.SafeIncrement(multigas.ResourceKindHistoryGrowth, dataHist); overflow {
			return multigas.ZeroGas(), ErrGasUintOverflow
		}
		return multiGas, nil
	}
}

func gasKeccak256(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	wordGas, overflow := stack.Back(1).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if wordGas, overflow = math.SafeMul(toWordSize(wordGas), params.Keccak256WordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	// TODO(NIT-3484): Update multi dimensional gas here
	if overflow = multiGas.SafeIncrement(multigas.ResourceKindUnknown, wordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

// pureMemoryGascost is used by several operations, which aside from their
// static cost have a dynamic cost which is solely based on the memory
// expansion
func pureMemoryGascost(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	return memoryGasCost(mem, memorySize)
}

var (
	gasReturn  = pureMemoryGascost
	gasRevert  = pureMemoryGascost
	gasMLoad   = pureMemoryGascost
	gasMStore8 = pureMemoryGascost
	gasMStore  = pureMemoryGascost
	gasCreate  = pureMemoryGascost
)

func gasCreate2(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	wordGas, overflow := stack.Back(2).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if wordGas, overflow = math.SafeMul(toWordSize(wordGas), params.Keccak256WordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	// Keccak hashing considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, wordGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasCreateEip3860(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	size, overflow := stack.Back(2).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if size > evm.chainConfig.MaxInitCodeSize() {
		return multigas.ZeroGas(), fmt.Errorf("%w: size %d", ErrMaxInitCodeSizeExceeded, size)
	}
	// Since size <= params.MaxInitCodeSize, these multiplication cannot overflow
	moreGas := params.InitCodeWordGas * ((size + 31) / 32)

	// Init code execution considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, moreGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}
func gasCreate2Eip3860(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	size, overflow := stack.Back(2).Uint64WithOverflow()
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	if size > evm.chainConfig.MaxInitCodeSize() {
		return multigas.ZeroGas(), fmt.Errorf("%w: size %d", ErrMaxInitCodeSizeExceeded, size)
	}
	// Since size <= params.MaxInitCodeSize, these multiplication cannot overflow
	moreGas := (params.InitCodeWordGas + params.Keccak256WordGas) * ((size + 31) / 32)

	// Init code execution and Keccak hashing both considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, moreGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	return multiGas, nil
}

func gasExpFrontier(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	expByteLen := uint64((stack.data[stack.len()-2].BitLen() + 7) / 8)

	var (
		gas      = expByteLen * params.ExpByteFrontier // no overflow check required. Max is 256 * ExpByte gas
		overflow bool
	)
	if gas, overflow = math.SafeAdd(gas, params.ExpGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	// TODO(NIT-3484): Update multi dimensional gas here
	return multigas.UnknownGas(gas), nil
}

func gasExpEIP158(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	expByteLen := uint64((stack.data[stack.len()-2].BitLen() + 7) / 8)

	var (
		gas      = expByteLen * params.ExpByteEIP158 // no overflow check required. Max is 256 * ExpByte gas
		overflow bool
	)
	if gas, overflow = math.SafeAdd(gas, params.ExpGas); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	// TODO(NIT-3484): Update multi dimensional gas here
	return multigas.UnknownGas(gas), nil
}

func gasCall(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	var (
		multiGas       = multigas.ZeroGas()
		transfersValue = !stack.Back(2).IsZero()
		address        = common.Address(stack.Back(1).Bytes20())
	)

	// Storage slot writes (zero → nonzero) considered as storage growth.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if evm.chainRules.IsEIP158 {
		if transfersValue && evm.StateDB.Empty(address) {
			multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CallNewAccountGas)
		}
	} else if !evm.StateDB.Exist(address) {
		multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CallNewAccountGas)
	}

	// Value transfer to non-empty account considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if transfersValue && !evm.chainRules.IsEIP4762 {
		multiGas.SafeIncrement(multigas.ResourceKindComputation, params.CallValueTransferGas)
	}

	memoryMultiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	multiGas, overflow := multiGas.SafeAdd(multiGas, memoryMultiGas)
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}

	singleGas := multiGas.SingleGas()
	evm.callGasTemp, err = callGas(evm.chainRules.IsEIP150, contract.Gas, singleGas, stack.Back(0))
	if err != nil {
		return multigas.ZeroGas(), err
	}
	// Call gas forwarding considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, evm.callGasTemp); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}

	return multiGas, nil
}

func gasCallCode(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	memoryMultiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	var (
		multiGas = multigas.ZeroGas()
		overflow bool
	)
	if stack.Back(2).Sign() != 0 && !evm.chainRules.IsEIP4762 {
		// Value transfer to non-empty account considered as computation.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		multiGas.SafeIncrement(multigas.ResourceKindComputation, params.CallValueTransferGas)
	}
	multiGas, overflow = multiGas.SafeAdd(multiGas, memoryMultiGas)
	if overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}
	singleGas := multiGas.SingleGas()
	evm.callGasTemp, err = callGas(evm.chainRules.IsEIP150, contract.Gas, singleGas, stack.Back(0))
	if err != nil {
		return multigas.ZeroGas(), err
	}
	// Call gas forwarding considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow = multiGas.SafeIncrement(multigas.ResourceKindComputation, evm.callGasTemp); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}

	return multiGas, nil
}

func gasDelegateCall(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	gas := multiGas.SingleGas()
	evm.callGasTemp, err = callGas(evm.chainRules.IsEIP150, contract.Gas, gas, stack.Back(0))
	if err != nil {
		return multigas.ZeroGas(), err
	}
	// Call gas forwarding considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow := multiGas.SafeIncrement(multigas.ResourceKindComputation, evm.callGasTemp); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}

	return multiGas, nil
}

func gasStaticCall(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas, err := memoryGasCost(mem, memorySize)
	if err != nil {
		return multigas.ZeroGas(), err
	}
	gas := multiGas.SingleGas()
	evm.callGasTemp, err = callGas(evm.chainRules.IsEIP150, contract.Gas, gas, stack.Back(0))
	if err != nil {
		return multigas.ZeroGas(), err
	}
	// Call gas forwarding considered as computation.
	// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
	if overflow := multiGas.SafeIncrement(multigas.ResourceKindComputation, evm.callGasTemp); overflow {
		return multigas.ZeroGas(), ErrGasUintOverflow
	}

	return multiGas, nil
}

func gasSelfdestruct(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (*multigas.MultiGas, error) {
	multiGas := multigas.ZeroGas()
	// EIP150 homestead gas reprice fork:
	if evm.chainRules.IsEIP150 {
		// Selfdestruct operation considered as storage access.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		multiGas.SafeIncrement(multigas.ResourceKindStorageAccess, params.SelfdestructGasEIP150)
		var address = common.Address(stack.Back(0).Bytes20())

		// New account creation considered as storage growth.
		// See rationale in: https://github.com/OffchainLabs/nitro/blob/master/docs/decisions/0002-multi-dimensional-gas-metering.md
		if evm.chainRules.IsEIP158 {
			// if empty and transfers value
			if evm.StateDB.Empty(address) && evm.StateDB.GetBalance(contract.Address()).Sign() != 0 {
				multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
			}
		} else if !evm.StateDB.Exist(address) {
			multiGas.SafeIncrement(multigas.ResourceKindStorageGrowth, params.CreateBySelfdestructGas)
		}
	}

	if !evm.StateDB.HasSelfDestructed(contract.Address()) {
		evm.StateDB.AddRefund(params.SelfdestructRefundGas)
	}
	return multiGas, nil
}
